const std = @import("std");

pub fn build(b: *std.Build) void {
    const exe = b.addExecutable(.{
        .name = "demo",
        .root_module = b.createModule(.{ .root_source_file = b.path("src/main.zig") }),
    });
    b.installArtifact(exe);
}
