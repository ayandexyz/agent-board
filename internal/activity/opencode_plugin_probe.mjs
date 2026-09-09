// Drives the board's OpenCode plugin without OpenCode: loads it and feeds it
// one event, so its posts land on whatever AGENTBOARD_HOOK_URL points at.
// Used by the Go tests.
const [, , pluginPath, eventJSON] = process.argv
const { server } = await import(pluginPath)
const hooks = await server({ client: {} })
await hooks.event({ event: JSON.parse(eventJSON) })
