// Stand-in for the board's hook receiver (Phase 2), so the FreeCode side can be
// watched end to end today. Prints every hook event; auto-answers permission
// requests after a delay so you can see a remote approval land in the pane.
//
//   node fake-board.mjs [port] [allow|deny|ignore]
//
import { createServer } from "http";

const port = Number(process.argv[2] ?? 8899);
const mode = process.argv[3] ?? "allow";
const started = Date.now();

const stamp = () => `+${((Date.now() - started) / 1000).toFixed(1)}s`.padStart(7);

createServer((req, res) => {
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", async () => {
    let p = {};
    try {
      p = JSON.parse(body || "{}");
    } catch {
      p = { unparsed: body };
    }

    const session = req.url.split("/").pop();
    const ev = p.hook_event_name ?? "?";
    const extra = [
      p.tool_name && `tool=${p.tool_name}`,
      p.tool_use_id && `id=${p.tool_use_id}`,
      p.notification_type && `type=${p.notification_type}`,
      p.tool_input && `input=${JSON.stringify(p.tool_input).slice(0, 90)}`,
    ]
      .filter(Boolean)
      .join("  ");

    console.log(`${stamp()}  ${session}  ${ev.padEnd(17)} ${extra}`);

    res.setHeader("content-type", "application/json");

    if (ev === "PermissionRequest" && mode !== "ignore") {
      console.log(`         -> holding the request, will answer "${mode}" in 3s`);
      await new Promise((r) => setTimeout(r, 3000));
      console.log(`         -> answering ${mode}`);
      res.end(
        JSON.stringify({
          hookSpecificOutput: { hookEventName: "PermissionRequest", decision: mode },
        }),
      );
      return;
    }

    if (ev === "Notification" && p.notification_type === "agent_needs_input") {
      console.log(`         -> THIS is where the board would raise "needs your answer"`);
    }

    res.end("{}");
  });
}).listen(port, "127.0.0.1", () => {
  console.log(`fake board listening on http://127.0.0.1:${port}  (permissions: ${mode})`);
  console.log(`run an agent with:  FREECODE_HOOK_URL=http://127.0.0.1:${port}/hooks/SES-01 freecode`);
  console.log("");
});
