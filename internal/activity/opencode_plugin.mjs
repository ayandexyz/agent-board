// AgentBoard activity reporter for OpenCode.
//
// Loaded per pane through OPENCODE_CONFIG. It posts Claude-Code-shaped hook
// payloads to the board's receiver so no board-side mapper is needed, and
// answers permission requests remotely by replying through OpenCode's own
// session API. Inert unless the board exported its environment.

const base = process.env.AGENTBOARD_HOOK_URL
const sessionID = process.env.AGENTBOARD_SESSION_ID
const launchID = process.env.AGENTBOARD_LAUNCH_ID ?? ""

export const server = async ({ client }) => {
  if (!base || !sessionID) return {}

  const url = `${base.replace(/\/+$/, "")}/hooks/${sessionID}`

  // Lifecycle reports are advisory: a board that went away must never stall a
  // turn, so they time out and swallow their errors.
  const post = async (body, timeoutMs) => {
    try {
      const res = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Agentboard-Launch": launchID },
        body: JSON.stringify({ session_id: sessionID, ...body }),
        signal: AbortSignal.timeout(timeoutMs),
      })
      return await res.json()
    } catch {
      return undefined
    }
  }

  const report = (body) => post(body, 5_000)

  // The board writes Claude Code's nested shape or FreeCode's bare string
  // depending on the agent it recorded. Read either.
  const decisionOf = (reply) => {
    const d = reply?.hookSpecificOutput?.decision
    return typeof d === "string" ? d : d?.behavior
  }

  const askPermission = async (info) => {
    const reply = await post(
      {
        hook_event_name: "PermissionRequest",
        tool_name: info.tool ?? info.permission ?? info.title ?? "permission",
        tool_input: info.metadata ?? {},
      },
      // A human is deciding. Outlast the board's own hold, then fall back to
      // the pane picker.
      11 * 60_000,
    )
    const decision = decisionOf(reply)
    if (decision !== "allow" && decision !== "deny") return
    try {
      await client.postSessionIdPermissionsPermissionId({
        path: { id: info.sessionID, permissionID: info.id },
        body: { response: decision === "allow" ? "once" : "reject" },
      })
    } catch {
      // The pane picker raced us to it, or the request is already gone.
    }
  }

  await report({ hook_event_name: "SessionStart" })

  return {
    "chat.message": async () => {
      await report({ hook_event_name: "UserPromptSubmit" })
    },
    "tool.execute.before": async (input, output) => {
      await report({
        hook_event_name: "PreToolUse",
        tool_name: input.tool,
        tool_use_id: input.callID,
        tool_input: output?.args ?? {},
      })
    },
    "tool.execute.after": async (input) => {
      await report({
        hook_event_name: "PostToolUse",
        tool_name: input.tool,
        tool_use_id: input.callID,
        tool_input: input.args ?? {},
      })
    },
    event: async ({ event }) => {
      switch (event.type) {
        case "session.idle":
          await report({ hook_event_name: "Stop" })
          return
        // The event was renamed across OpenCode versions; both carry the
        // pending request as their properties.
        case "permission.asked":
        case "permission.updated":
          await askPermission(event.properties)
          return
      }
    },
  }
}
