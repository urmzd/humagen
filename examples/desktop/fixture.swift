import AppKit

// An isolated native target for the opt-in MCP acceptance. It writes only to
// the receipt paths the test supplies and has no other application data.
final class DesktopFixture: NSObject, NSApplicationDelegate {
    var window: NSWindow!
    let input = NSTextField(string: "before")
    let status = NSTextField(labelWithString: "Before acceptance")

    func applicationDidFinishLaunching(_ notification: Notification) {
        window = NSWindow(contentRect: NSRect(x: 40, y: 40, width: 640, height: 360),
                          styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.title = "Humagen Desktop Acceptance"
        window.backgroundColor = NSColor(srgbRed: 0.12, green: 0.6, blue: 0.25, alpha: 1)
        let content = window.contentView!
        status.frame = NSRect(x: 32, y: 270, width: 560, height: 36)
        status.font = .systemFont(ofSize: 24)
        status.textColor = .white
        status.setAccessibilityIdentifier("fixture-status")
        input.frame = NSRect(x: 32, y: 200, width: 400, height: 32)
        input.setAccessibilityIdentifier("fixture-input")
        let button = NSButton(title: "Change fixture", target: self, action: #selector(change))
        button.frame = NSRect(x: 32, y: 130, width: 180, height: 40)
        button.bezelStyle = .rounded
        button.setAccessibilityIdentifier("fixture-change")
        content.addSubview(status)
        content.addSubview(input)
        content.addSubview(button)
        window.orderFront(nil)
        write(["window_id": window.windowNumber, "pid": ProcessInfo.processInfo.processIdentifier],
              to: CommandLine.arguments[1])
    }

    @objc func change() {
        status.stringValue = "After acceptance"
        write(["value": input.stringValue, "status": status.stringValue], to: CommandLine.arguments[2])
    }

    func write(_ value: [String: Any], to path: String) {
        if let data = try? JSONSerialization.data(withJSONObject: value) {
            try? data.write(to: URL(fileURLWithPath: path), options: .atomic)
        }
    }
}

let application = NSApplication.shared
let delegate = DesktopFixture()
application.setActivationPolicy(.accessory)
application.delegate = delegate
application.run()
