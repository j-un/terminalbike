# Terminal Bike

![title](assets/title.png)
![gameplay](assets/gameplay.png)

Exciting bike racing game in your terminal.

## Install

Download the archive for your OS and architecture from the [Releases](https://github.com/j-un/terminalbike/releases) page, extract it, and place the `terminalbike` binary somewhere in your `PATH`.

### Build from source

Requires [Go](https://go.dev/) to be installed.

```sh
make build
```

## Run

```sh
terminalbike
```

Recommended terminal size: **80×24 or larger**. Wider screens are more fun!

## Controls

| Key     | Action      |
| ------- | ----------- |
| ↑ / ↓   | Change lane |
| →       | Accelerate  |
| ←       | Brake       |
| Space   | Turbo       |
| q / Esc | Quit        |

## Objects

| Symbol | Meaning                                |
| ------ | -------------------------------------- |
| `#`    | Block — crash on contact               |
| `/`    | Ramp — jump over obstacles             |
| `»`    | Cool zone — instantly cools the engine |
| `~`    | Mud — resets speed to default          |
| `@`    | Rival — crash on contact               |
