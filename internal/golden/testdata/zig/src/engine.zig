const strings = @import("util/strings.zig");

pub const Engine = struct {
    const Self = @This();

    ticks: u64 = 0,

    pub fn init() Self {
        return .{};
    }

    pub fn run(self: *Self) void {
        self.step();
        self.ticks += 1;
        _ = strings.upper("x");
    }

    fn step(self: *Self) void {
        _ = self;
    }
};
