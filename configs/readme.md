# Configuration Files

SNI AutoSplitter uses a two-tier configuration system to define autosplitting conditions and run categories.

## Directory Structure

```
configs/
├── games/          # Game-specific memory address definitions
│   ├── alttp.json
│   └── super-metroid.json
└── runs/           # Run category definitions that reference game splits
    ├── alttp-any-nmg.json
    ├── alttp-100.json
    └── super-metroid-any.json
```

## Game Configuration Format

Game configurations define memory addresses and conditions for autosplitting. They are compatible with the USB2SNES autosplitter format and use SNES A-bus addresses, which are automatically converted to FxPakPro address space internally.

### Basic Structure

```json
{
  "name": "Game Name",
  "autostart": {
    "name": "Autostart",
    "note": "Optional description",
    "address": "0x10",
    "value": "0x05", 
    "type": "eq"
  },
  "definitions": [
    {
      "name": "Split Name",
      "note": "Optional description",
      "address": "0xF374",
      "value": "0x04",
      "type": "bit"
    }
  ]
}
```

### Split Condition Types

| Type   | Description                    | Value Range |
|--------|--------------------------------|-------------|
| `eq`   | Byte equal                     | 0x00-0xFF   |
| `weq`  | Word (16-bit) equal            | 0x0000-0xFFFF |
| `bit`  | Byte bit set                   | 0x01-0x80   |
| `wbit` | Word bit set                   | 0x0001-0x8000 |
| `lt`   | Byte less than                 | 0x00-0xFF   |
| `wlt`  | Word less than                 | 0x0000-0xFFFF |
| `lte`  | Byte less than or equal        | 0x00-0xFF   |
| `wlte` | Word less than or equal        | 0x0000-0xFFFF |
| `gt`   | Byte greater than              | 0x00-0xFF   |
| `wgt`  | Word greater than              | 0x0000-0xFFFF |
| `gte`  | Byte greater than or equal     | 0x00-0xFF   |
| `wgte` | Word greater than or equal     | 0x0000-0xFFFF |

### Advanced Conditions

#### Multiple Conditions (`more`)

Use `more` to require multiple memory conditions to be true simultaneously:

```json
{
  "name": "Complex Split",
  "address": "0xF374",
  "value": "0x04",
  "type": "bit",
  "more": [
    {
      "name": "Additional Check",
      "address": "0x100",
      "value": "0x96", 
      "type": "eq"
    }
  ]
}
```

#### Sequential Conditions (`next`)

Use `next` to require a sequence of conditions to be met in order:

```json
{
  "name": "Sequential Split",
  "address": "0xA0",
  "value": "0x12",
  "type": "eq",
  "next": [
    {
      "name": "Then This",
      "address": "0x11",
      "value": "0x00",
      "type": "eq"
    }
  ]
}
```

**Note:** A split cannot have both `more` and `next` conditions.

### Address Space

Game configurations use **SNES A-bus addresses** (the native SNES memory addressing format). These addresses are automatically converted to FxPakPro address space internally by adding `0xF50000` to map them to the WRAM region.

For example:
- `0xA0` (game mode) → `0xF500A0` in FxPakPro space
- `0x10` (submodule) → `0xF50010` in FxPakPro space  
- `0x130` (room ID) → `0xF50130` in FxPakPro space
- `0xF374` (item flags) → `0xF5F374` in FxPakPro space

The FxPakPro address space mapping is:
- `$00_0000..$DF_FFFF` = ROM contents
- `$E0_0000..$EF_FFFF` = SRAM contents  
- `$F5_0000..$F6_FFFF` = WRAM contents (where most game variables are stored)
- `$F7_0000..$F7_FFFF` = VRAM contents
- `$F8_0000..$F8_FFFF` = APU contents
- `$F9_0000..$F9_01FF` = CGRAM contents
- `$F9_0200..$F9_041F` = OAM contents

Since most SNES game variables are stored in WRAM, and the USB2SNES protocol maps SNES addresses to this WRAM region, you should use standard SNES A-bus addresses in your game configurations.

## Run Configuration Format

Run configurations define which splits to use for a specific speedrun category.

### Basic Structure

```json
{
  "name": "Full Display Name",
  "category": "Category Name",
  "game": "game-config-name",
  "splits": [
    "Split Name 1",
    "Split Name 2",
    "Split Name 3"
  ],
  "disableAutostart": false
}
```

### Fields

- `name`: Full descriptive name displayed to user
- `category`: Short category name used for matching command line arguments
- `game`: Must match the filename of a game config (without .json extension)
- `splits`: Array of split names that must exist in the referenced game config
- `disableAutostart` (optional, default `false`): If `true`, the run ignores the game config's `autostart` condition and waits in the "waiting for start" state until started manually (e.g. by sending a `Started` event from a connected LiveSplit client). Useful for run configs that begin mid-game, where the game's normal autostart condition wouldn't apply.

## Validation

All configuration files are strictly validated:

- Required fields must be present
- Memory addresses must be valid hexadecimal
- Split types must be recognized
- Value ranges must match the operation type
- Split names must be unique within a game
- Run splits must reference existing game splits
- No nested `more` or `next` conditions in sub-splits

Invalid configurations will cause the program to exit with detailed error messages.

## Examples

See the example configurations in this directory:
- `games/alttp.json` - A Link to the Past game configuration
- `runs/alttp-any-nmg.json` - Any% No Major Glitches run
- `runs/alttp-100.json` - 100% completion run