# goblin

## Goblin Decoder
Desktop application for real-time decoding of **Goblin** payload, which includes images and telemetry from high-altitude balloon payloads.

### Quick Start

```bash
cd decoder-gui
wails dev      # development mode with hot reload
wails build    # production build
```

On first run the app creates a `config.json` next to the binary. Edit it to match your APID/port assignments, then restart. By default they're configured to work with pipelines defined in [GobDump](https://github.com/MrTalon63/GobDump).