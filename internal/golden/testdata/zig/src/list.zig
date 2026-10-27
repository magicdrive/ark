pub fn List(comptime T: type) type {
    return struct {
        const Self = @This();

        items: []T = &.{},

        pub fn count(self: Self) usize {
            return self.items.len;
        }

        pub fn push(self: *Self, item: T) void {
            _ = item;
            _ = self.count();
        }
    };
}
