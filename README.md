# quicksetd

A minimal Zig CLI application demonstrating best practices for project structure.

## Features

- Clean, idiomatic Zig code
- Standard project structure following Zig conventions
- Simple "Hello World" CLI application
- Ready for extension and development

## Project Structure

```
quicksetd/
├── build.zig          # Build configuration
├── build.zig.zon      # Package manifest
├── src/
│   ├── main.zig       # CLI entry point
│   └── root.zig       # Library root (public API)
├── LICENSE
└── README.md
```

## Requirements

- Zig 0.13.0 or later

## Building

```bash
zig build
```

The compiled binary will be in `zig-out/bin/quicksetd`.

## Running

Run directly:
```bash
zig build run
```

Or run the compiled binary:
```bash
./zig-out/bin/quicksetd
```

## Testing

Run all tests:
```bash
zig build test
```

## Development

This project follows Zig best practices:

- **`build.zig`**: Uses the standard build system with proper target and optimization options
- **`build.zig.zon`**: Package manifest for dependency management (ready for future use)
- **`src/main.zig`**: Entry point for the executable, kept minimal and focused
- **`src/root.zig`**: Library root for reusable code and public API
- **Clean separation**: CLI logic in `main.zig`, library code in `root.zig`

## License

See [LICENSE](LICENSE) file for details.