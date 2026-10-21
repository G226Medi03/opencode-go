# opencode-go

Go client for driving OpenCode V2 through its CLI (`opencode run --format json`).

It is the OpenCode transport layer used by [raven](https://github.com/G226Medi03/raven).
It does not call model providers directly: every model call goes through OpenCode.

## Install

```sh
go get github.com/G226Medi03/opencode-go
```

## Usage

```go
client := &opencode.Client{}
result, err := client.Run(ctx, opencode.AgentRequest{
    Agent:  "my-agent",
    Model:  "opencode-go/deepseek-v4.1-flash",
    Prompt: "do the task",
    Dir:    ".",
})
```

`Run` streams NDJSON events through `AgentRequest.EventWriter` / `OnEvent`, and returns
the final text, tool calls, steps, usage and session id.

## API

- `Runner` / `Client.Run`
- `AgentRequest`, `AgentResult`
- `Event`, `ParseEvent`, `ParseStream`
- `Usage`, `ToolCall`
- `Args`
- `DetectVersion`

## Security

- OpenCode child processes do not inherit `RAVEN_*` environment variables.
- Prompt/log persistence is the caller's responsibility; this package only runs the CLI.

## License

MIT. See [LICENSE](LICENSE).
