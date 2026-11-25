import AVFoundation
import CoreAudio
import Foundation

struct Arguments {
    var socketPath = ""
    var bundleID = "com.now.gg.BlueStacks"
    var bitRate = 128_000
    var waitSeconds = 30.0

    init(_ argv: [String]) {
        var it = argv.dropFirst().makeIterator()
        while let flag = it.next() {
            guard let value = it.next() else { break }
            switch flag {
            case "--socket": socketPath = value
            case "--bundle": bundleID = value
            case "--bitrate": bitRate = Int(value) ?? bitRate
            case "--wait": waitSeconds = Double(value) ?? waitSeconds
            default: fail("unknown flag \(flag)")
            }
        }
        if socketPath.isEmpty { fail("--socket is required") }
    }
}

func log(_ message: String) {
    FileHandle.standardError.write(Data("audiotap: \(message)\n".utf8))
}

func fail(_ message: String) -> Never {
    log(message)
    exit(1)
}

func check(_ status: OSStatus, _ what: String) {
    if status != noErr { fail("\(what) failed with status \(status)") }
}

func globalAddress(_ selector: AudioObjectPropertySelector) -> AudioObjectPropertyAddress {
    AudioObjectPropertyAddress(mSelector: selector, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
}

func processObjects() -> [AudioObjectID] {
    var addr = globalAddress(kAudioHardwarePropertyProcessObjectList)
    var size: UInt32 = 0
    let system = AudioObjectID(kAudioObjectSystemObject)
    guard AudioObjectGetPropertyDataSize(system, &addr, 0, nil, &size) == noErr else { return [] }
    var ids = [AudioObjectID](repeating: 0, count: Int(size) / MemoryLayout<AudioObjectID>.size)
    guard AudioObjectGetPropertyData(system, &addr, 0, nil, &size, &ids) == noErr else { return [] }
    return ids
}

func bundleID(of process: AudioObjectID) -> String? {
    var addr = globalAddress(kAudioProcessPropertyBundleID)
    var value: Unmanaged<CFString>?
    var size = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
    guard AudioObjectGetPropertyData(process, &addr, 0, nil, &size, &value) == noErr, let v = value else { return nil }
    return v.takeRetainedValue() as String
}

func waitForProcesses(bundle: String, timeout: Double) -> [AudioObjectID] {
    let deadline = Date().addingTimeInterval(timeout)
    while true {
        let found = processObjects().filter { bundleID(of: $0) == bundle }
        if !found.isEmpty { return found }
        if Date() > deadline { fail("no audio process for \(bundle) after \(timeout)s") }
        Thread.sleep(forTimeInterval: 0.5)
    }
}

func connect(to path: String) -> Int32 {
    let fd = socket(AF_UNIX, SOCK_STREAM, 0)
    if fd < 0 { fail("socket: \(errno)") }
    var addr = sockaddr_un()
    addr.sun_family = sa_family_t(AF_UNIX)
    let capacity = MemoryLayout.size(ofValue: addr.sun_path)
    guard path.utf8.count < capacity else { fail("socket path too long") }
    withUnsafeMutableBytes(of: &addr.sun_path) { raw in
        raw.copyBytes(from: path.utf8)
        raw[path.utf8.count] = 0
    }
    let result = withUnsafePointer(to: &addr) {
        $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
    }
    if result != 0 { fail("connect \(path): \(errno)") }
    var on: Int32 = 1
    setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &on, socklen_t(MemoryLayout<Int32>.size))
    return fd
}

func writeAll(_ fd: Int32, _ data: Data) -> Bool {
    data.withUnsafeBytes { raw in
        var offset = 0
        while offset < raw.count {
            let n = Darwin.write(fd, raw.baseAddress! + offset, raw.count - offset)
            if n <= 0 { return false }
            offset += n
        }
        return true
    }
}

final class OpusEncoder {
    private let converter: AVAudioConverter
    private let outputFormat: AVAudioFormat
    private var pending: [AVAudioPCMBuffer] = []
    private var packetIndex: UInt64 = 0
    private let send: (UInt64, Data) -> Void

    init(input: AVAudioFormat, bitRate: Int, send: @escaping (UInt64, Data) -> Void) {
        var desc = AudioStreamBasicDescription(
            mSampleRate: 48000, mFormatID: kAudioFormatOpus, mFormatFlags: 0, mBytesPerPacket: 0,
            mFramesPerPacket: 960, mBytesPerFrame: 0, mChannelsPerFrame: 2, mBitsPerChannel: 0, mReserved: 0)
        outputFormat = AVAudioFormat(streamDescription: &desc)!
        guard let conv = AVAudioConverter(from: input, to: outputFormat) else { fail("no Opus converter for \(input)") }
        conv.bitRate = bitRate
        converter = conv
        self.send = send
    }

    func encode(_ buffer: AVAudioPCMBuffer) {
        pending.append(buffer)
        while true {
            let out = AVAudioCompressedBuffer(format: outputFormat, packetCapacity: 1, maximumPacketSize: converter.maximumOutputPacketSize)
            var error: NSError?
            let status = converter.convert(to: out, error: &error) { _, inputStatus in
                if self.pending.isEmpty {
                    inputStatus.pointee = .noDataNow
                    return nil
                }
                inputStatus.pointee = .haveData
                return self.pending.removeFirst()
            }
            if status == .error { fail("opus encode: \(error?.localizedDescription ?? "unknown")") }
            guard out.packetCount > 0 else { return }
            send(packetIndex * 20_000, Data(bytes: out.data, count: Int(out.byteLength)))
            packetIndex += 1
        }
    }
}

let args = Arguments(CommandLine.arguments)
let targets = waitForProcesses(bundle: args.bundleID, timeout: args.waitSeconds)
log("tapping \(args.bundleID) (audio objects \(targets))")

let tapDescription = CATapDescription(stereoMixdownOfProcesses: targets)
tapDescription.muteBehavior = .muted
tapDescription.isPrivate = true
tapDescription.name = "cloudphone-audiotap"
var tapID = AudioObjectID(kAudioObjectUnknown)
check(AudioHardwareCreateProcessTap(tapDescription, &tapID), "create process tap")

var formatAddress = globalAddress(kAudioTapPropertyFormat)
var tapFormat = AudioStreamBasicDescription()
var formatSize = UInt32(MemoryLayout<AudioStreamBasicDescription>.size)
check(AudioObjectGetPropertyData(tapID, &formatAddress, 0, nil, &formatSize, &tapFormat), "read tap format")
guard let inputFormat = AVAudioFormat(streamDescription: &tapFormat) else { fail("unsupported tap format") }
log("tap format \(inputFormat)")

let aggregate: [String: Any] = [
    kAudioAggregateDeviceNameKey: "cloudphone-audiotap",
    kAudioAggregateDeviceUIDKey: UUID().uuidString,
    kAudioAggregateDeviceIsPrivateKey: true,
    kAudioAggregateDeviceTapAutoStartKey: true,
    kAudioAggregateDeviceTapListKey: [[kAudioSubTapUIDKey: tapDescription.uuid.uuidString, kAudioSubTapDriftCompensationKey: true]],
]
var deviceID = AudioObjectID(kAudioObjectUnknown)
check(AudioHardwareCreateAggregateDevice(aggregate as CFDictionary, &deviceID), "create aggregate device")

let fd = connect(to: args.socketPath)
let encoder = OpusEncoder(input: inputFormat, bitRate: args.bitRate) { pts, payload in
    var header = Data(count: 12)
    header.withUnsafeMutableBytes { raw in
        raw.storeBytes(of: pts.bigEndian, toByteOffset: 0, as: UInt64.self)
        raw.storeBytes(of: UInt32(payload.count).bigEndian, toByteOffset: 8, as: UInt32.self)
    }
    if !writeAll(fd, header + payload) {
        log("bridge disconnected")
        exit(0)
    }
}

let queue = DispatchQueue(label: "cloudphone.audiotap")
var procID: AudioDeviceIOProcID?
check(AudioDeviceCreateIOProcIDWithBlock(&procID, deviceID, queue) { _, inputData, _, _, _ in
    guard let view = AVAudioPCMBuffer(pcmFormat: inputFormat, bufferListNoCopy: inputData, deallocator: nil),
          let copy = AVAudioPCMBuffer(pcmFormat: inputFormat, frameCapacity: view.frameLength) else { return }
    copy.frameLength = view.frameLength
    let src = UnsafeMutableAudioBufferListPointer(UnsafeMutablePointer(mutating: view.audioBufferList))
    let dst = UnsafeMutableAudioBufferListPointer(copy.mutableAudioBufferList)
    for (s, d) in zip(src, dst) where s.mData != nil && d.mData != nil {
        memcpy(d.mData, s.mData, Int(min(s.mDataByteSize, d.mDataByteSize)))
    }
    encoder.encode(copy)
}, "create IO proc")
check(AudioDeviceStart(deviceID, procID), "start device")
log("streaming Opus at \(args.bitRate) bit/s")

for sig in [SIGTERM, SIGINT] {
    signal(sig, SIG_IGN)
    let source = DispatchSource.makeSignalSource(signal: sig, queue: .main)
    source.setEventHandler {
        AudioDeviceStop(deviceID, procID)
        AudioHardwareDestroyAggregateDevice(deviceID)
        AudioHardwareDestroyProcessTap(tapID)
        exit(0)
    }
    source.resume()
    _ = Unmanaged.passRetained(source as AnyObject)
}
dispatchMain()
