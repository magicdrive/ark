const builtin = @import("builtin");
const posix = @import("posix.zig");
const windows = @import("windows.zig");

pub const Backend = if (builtin.os.tag == .windows) windows.Backend else posix.Backend;
