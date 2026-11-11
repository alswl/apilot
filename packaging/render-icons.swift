import AppKit

// Keep the geometric mark identical in the app icon and the menu bar template.
func mark(_ color: NSColor) {
    color.setFill()
    let arrow = NSBezierPath()
    arrow.move(to: NSPoint(x: 49, y: 76))
    arrow.line(to: NSPoint(x: 70, y: 27))
    arrow.line(to: NSPoint(x: 49, y: 38))
    arrow.line(to: NSPoint(x: 28, y: 27))
    arrow.close()
    arrow.fill()
    let orbit = NSBezierPath()
    orbit.appendArc(withCenter: NSPoint(x: 49, y: 49), radius: 34, startAngle: 35, endAngle: 300)
    orbit.lineWidth = 5
    orbit.lineCapStyle = .round
    color.setStroke()
    orbit.stroke()
    NSBezierPath(ovalIn: NSRect(x: 73, y: 68, width: 8, height: 8)).fill()
}
func render(_ size: Int, template: Bool, to path: String) throws {
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    let scale = CGFloat(size) / 100
    let transform = NSAffineTransform()
    transform.scale(by: scale)
    transform.concat()
    if !template {
        let bg = NSBezierPath(roundedRect: NSRect(x: 4, y: 4, width: 92, height: 92), xRadius: 22, yRadius: 22)
        NSGradient(colors: [NSColor(srgbRed: 0.12, green: 0.34, blue: 0.98, alpha: 1), NSColor(srgbRed: 0.48, green: 0.19, blue: 0.94, alpha: 1), NSColor(srgbRed: 0.06, green: 0.76, blue: 0.86, alpha: 1)])!.draw(in: bg, angle: 40)
    }
    mark(template ? .black : .white)
    NSGraphicsContext.restoreGraphicsState()
    try rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: path))
}
try render(1024, template: false, to: "resources/icon.png")
try render(128, template: false, to: "internal/ui/assets/app.png")
try render(32, template: true, to: "internal/ui/assets/tray.png")
