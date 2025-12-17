const std = @import("std");

pub fn main() !void {
    // Setup stdout writer
    const stdout = std.io.getStdOut().writer();

    // Print hello message
    try stdout.print("Hello, World!\n", .{});
    try stdout.print("Welcome to quicksetd - a Zig CLI application.\n", .{});
}

test "basic test" {
    try std.testing.expectEqual(10, 3 + 7);
}
