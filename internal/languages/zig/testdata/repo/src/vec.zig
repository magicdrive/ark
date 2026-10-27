pub const Vec = struct {
    x: f32,
    y: f32,

    pub fn init(x: f32, y: f32) Vec {
        return .{ .x = x, .y = y };
    }

    pub fn length(self: Vec) f32 {
        return self.x + self.y;
    }
};
