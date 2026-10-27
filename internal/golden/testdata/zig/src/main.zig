const std = @import("std");
const engine = @import("engine.zig");
const math = @import("math.zig");
const list = @import("list.zig");
const platform = @import("platform.zig");

const Engine = engine.Engine;

pub fn main() !void {
    var e = Engine.init();
    e.run();
    _ = math.add(1, 2);
    var v = math.Vec.init(1, 2);
    _ = v.length();
    var l: list.List(u32) = .{};
    _ = l.count();
    platform.Backend.open();
    std.debug.print("{}\n", .{e.ticks});
}
