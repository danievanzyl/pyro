# Design System: Luminous Density

## Colors

| Token | Hex |
|-------|-----|
| background | #0f0d16 |
| surface | #0f0d16 |
| surface-container | #1b1823 |
| surface-container-high | #211e2a |
| surface-container-highest | #272431 |
| surface-container-low | #14121c |
| on-surface | #f5eefc |
| on-surface-variant | #aea9b6 |
| primary | #d394ff |
| primary-dim | #a343e7 |
| secondary | #ff6d88 |
| tertiary | #b5ffc2 |
| error | #ff6e84 |
| outline | #78737f |
| outline-variant | #494651 |

## Typography

- Headlines: Manrope (geometric, warm, authoritative)
- Body/Labels: Inter (high x-height, readable at small sizes)

## Surface Philosophy

- Glass panels: `rgba(27, 24, 35, 0.6)` + `backdrop-filter: blur(30px)`
- No solid borders — use tonal shifts for sectioning
- Ghost borders: 1px `outline-variant` at 20% opacity for definition
- Background gradients: radial from primary-dim at corners

## Status Colors

- Healthy/Running: tertiary (#b5ffc2)
- Warning: secondary (#ff6d88)
- Error: error (#ff6e84)
- Creating: primary (#d394ff)

## Roundness

- Cards: 0.75rem (xl)
- Buttons: 0.75rem or full (pill)
- Inputs: 0.5rem (lg)
