const std = @import("std");
const testing = std.testing;

/// Library root - export public API here
pub const version = "0.1.0";

/// Example function that can be used as a library
pub fn greet(allocator: std.mem.Allocator, name: []const u8) ![]const u8 {
    return std.fmt.allocPrint(allocator, "Hello, {s}!", .{name});
}

test "greet function" {
    const allocator = testing.allocator;
    const greeting = try greet(allocator, "Zig");
    defer allocator.free(greeting);
    
    try testing.expectEqualStrings("Hello, Zig!", greeting);
}
