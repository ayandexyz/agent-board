#!/usr/bin/env python3
"""Memory and CPU cost of AgentBoard against herdr while running N agents.

Both tools run the same agents, so what differs is the orchestrator: its own
processes, the panes it keeps, the screens it parses. The headline number is
the proportional set size (PSS) of the tool's processes alone — agentboard
plus its tmux server and client, herdr's client plus its server — with every
agent subtree and pane shell reported in their own buckets.

Workloads:
  stream   a stand-in `claude` that prints the Claude Code banner and then
           steady coloured output (default 20 lines/s per agent). Costs
           nothing, and makes both tools do the same screen work.
  claude   the real Claude Code, left at its prompt. Neither tool sends a
           prompt, so no tokens are spent. Where a pane shows the folder-trust
           dialog (whose default for a temp dir is "No, exit") the harness
           picks "Yes" in it, the same way for both tools; a pane without the
           dialog gets no keys, since Down + Enter at the prompt would resend
           an old prompt.

Isolation: agentboard gets its own tmux socket (TMUX_TMPDIR), database,
worktree root and API port; herdr gets its own named session. Pane shells
are `bash --norc` for both, so dotfiles cannot change what runs. Nothing is
left behind except Claude Code remembering it trusted the temp folders.

  python3 scripts/bench-memory.py --agents 1 5 10 --json-out bench.json
  python3 scripts/bench-memory.py --workload claude --agents 10

Linux only: it reads /proc.
"""
from __future__ import annotations

import argparse
import fcntl
import json
import os
import platform
import pty
import re
import select
import shutil
import signal
import socket
import statistics
import struct
import subprocess
import sys
import tempfile
import termios
import threading
import time
import urllib.request
import uuid
from pathlib import Path

SCHEMA = "agentboard-bench/memory@1"
CLK_TCK = os.sysconf("SC_CLK_TCK")
PAGE_KB = os.sysconf("SC_PAGE_SIZE") // 1024
SHELLS = {"bash", "sh", "zsh", "fish", "dash"}
AGENTS = {"claude"}
COLS, ROWS = 220, 50
TRUST_DIALOG = "trust this folder"

QUERY_REPLIES = [
    (b"\x1b[6n", b"\x1b[1;1R"),
    (b"\x1b[c", b"\x1b[?62;c"),
    (b"\x1b[>c", b"\x1b[>0;0;0c"),
    (b"\x1b]10;?\x1b\\", b"\x1b]10;rgb:ffff/ffff/ffff\x1b\\"),
    (b"\x1b]11;?\x1b\\", b"\x1b]11;rgb:0000/0000/0000\x1b\\"),
    (b"\x1b]10;?\x07", b"\x1b]10;rgb:ffff/ffff/ffff\x07"),
    (b"\x1b]11;?\x07", b"\x1b]11;rgb:0000/0000/0000\x07"),
    (b"\x1b[14t", b"\x1b[4;800;1760t"),
    (b"\x1b[16t", b"\x1b[6;16;8t"),
    (b"\x1b[18t", b"\x1b[8;50;220t"),
]

FAKE_AGENT = """#!/usr/bin/python3
import os, sys, time
rate = float(os.environ.get("BENCH_LINES_PER_SEC", "20"))
print("\\x1b[1mClaude Code v9.9.9\\x1b[0m  (benchmark stand-in)", flush=True)
i = 0
while True:
    i += 1
    sys.stdout.write("\\x1b[3%dm%6d\\x1b[0m reading src/module_%03d.go  %s\\n"
                     % (i % 7 + 1, i, i % 500, "#" * (i % 60)))
    sys.stdout.flush()
    time.sleep(1.0 / rate)
"""

PANE_SHELL = """#!/usr/bin/bash
[ "$1" = "-l" ] && shift
if [ "$1" = "-c" ]; then exec /usr/bin/bash --noprofile --norc "$@"; fi
exec /usr/bin/bash --noprofile --norc -i
"""


# --- /proc ------------------------------------------------------------------

def read_stat(pid: int) -> tuple[int, str, int] | None:
    """(ppid, comm, cpu ticks incl. reaped children) for one pid."""
    try:
        raw = Path(f"/proc/{pid}/stat").read_text()
    except OSError:
        return None
    comm = raw[raw.index("(") + 1:raw.rindex(")")]
    f = raw[raw.rindex(")") + 2:].split()
    return int(f[1]), comm, int(f[11]) + int(f[12]) + int(f[13]) + int(f[14])


def proc_table() -> dict[int, tuple[int, str, int]]:
    table = {}
    for entry in os.listdir("/proc"):
        if entry.isdigit():
            st = read_stat(int(entry))
            if st:
                table[int(entry)] = st
    return table


def pss_kb(pid: int) -> int:
    try:
        for line in Path(f"/proc/{pid}/smaps_rollup").read_text().splitlines():
            if line.startswith("Pss:"):
                return int(line.split()[1])
    except OSError:
        pass
    return 0


def descendants(roots: list[int], table: dict) -> set[int]:
    children: dict[int, list[int]] = {}
    for pid, (ppid, _, _) in table.items():
        children.setdefault(ppid, []).append(pid)
    seen, stack = set(), [r for r in roots if r in table]
    while stack:
        pid = stack.pop()
        if pid not in seen:
            seen.add(pid)
            stack.extend(children.get(pid, []))
    return seen


def classify(pids: set[int], table: dict) -> dict[int, str]:
    """agent: an agent or anything under one; shell: a pane shell; tool: the rest."""
    out = {}
    for pid in pids:
        bucket, cur = None, pid
        while cur in pids:
            if table[cur][1] in AGENTS:
                bucket = "agent"
                break
            cur = table[cur][0]
        if bucket is None:
            bucket = "shell" if table[pid][1] in SHELLS else "tool"
        out[pid] = bucket
    return out


def snapshot(roots: list[int]) -> dict:
    table = proc_table()
    pids = descendants(roots, table)
    kinds = classify(pids, table)
    snap = {b: {"pss_kb": 0, "procs": 0, "ticks": 0} for b in ("tool", "shell", "agent")}
    by_name: dict[str, int] = {}
    for pid, bucket in kinds.items():
        kb = pss_kb(pid)
        snap[bucket]["pss_kb"] += kb
        snap[bucket]["procs"] += 1
        snap[bucket]["ticks"] += table[pid][2]
        if bucket == "tool":
            by_name[table[pid][1]] = by_name.get(table[pid][1], 0) + kb
    snap["tool_by_process_kb"] = by_name
    snap["agents_running"] = sum(1 for pid, b in kinds.items()
                                 if b == "agent" and kinds.get(table[pid][0]) != "agent")
    return snap


# --- harness ----------------------------------------------------------------

class Client:
    """A tool's front end in a PTY, drained continuously so it never blocks."""

    def __init__(self, argv: list[str], env: dict, cwd: str):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(cwd)
            os.execvpe(argv[0], argv, env)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.bytes = 0
        self._stop = False
        threading.Thread(target=self._drain, daemon=True).start()

    def _drain(self) -> None:
        tail = b""
        while not self._stop:
            r, _, _ = select.select([self.fd], [], [], 0.2)
            if not r:
                continue
            try:
                chunk = os.read(self.fd, 65536)
            except OSError:
                return
            if not chunk:
                return
            self.bytes += len(chunk)
            tail = (tail + chunk)[-256:]
            for query, reply in QUERY_REPLIES:
                if query in tail:
                    os.write(self.fd, reply)
                    tail = tail.replace(query, b"")

    def close(self) -> None:
        self._stop = True
        for sig in (signal.SIGTERM, signal.SIGKILL):
            try:
                os.kill(self.pid, sig)
                time.sleep(0.5)
            except ProcessLookupError:
                break
        try:
            os.waitpid(self.pid, os.WNOHANG)
            os.close(self.fd)
        except OSError:
            pass


def wait_for(check, timeout: float, what: str):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        try:
            got = check()
            if got:
                return got
        except Exception:
            pass
        time.sleep(0.25)
    raise TimeoutError(f"timed out after {timeout:.0f}s waiting for {what}")


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def make_repo(root: Path) -> Path:
    repo = root / f"abbench-{uuid.uuid4().hex[:6]}"
    repo.mkdir()
    git = ["git", "-C", str(repo), "-c", "user.name=bench", "-c", "user.email=bench@localhost"]
    subprocess.run(["git", "init", "-q", "-b", "main", str(repo)], check=True)
    (repo / "README.md").write_text("benchmark repository\n")
    subprocess.run(git + ["add", "."], check=True)
    subprocess.run(git + ["commit", "-qm", "init"], check=True)
    return repo


def kill_under(path: Path) -> None:
    """Last resort: anything still running with its cwd inside the temp dir."""
    for entry in os.listdir("/proc"):
        if not entry.isdigit():
            continue
        try:
            if os.readlink(f"/proc/{entry}/cwd").startswith(str(path)):
                os.kill(int(entry), signal.SIGKILL)
        except OSError:
            pass


class AgentBoard:
    name = "agentboard"

    def __init__(self, binary: str):
        self.binary = binary

    def version(self) -> str:
        return subprocess.run([self.binary, "--version"], capture_output=True, text=True).stdout.strip()

    def start(self, repo: Path, tmp: Path, env: dict) -> None:
        self.repo, self.port, self.sessions = repo, free_port(), []
        self.env = dict(env)
        self.env.pop("TMUX", None)
        self.env.pop("TMUX_PANE", None)
        (tmp / "tmux").mkdir()
        self.env.update({
            "TMUX_TMPDIR": str(tmp / "tmux"),
            "AGENTBOARD_DB": str(tmp / "board.db"),
            "AGENTBOARD_GIT_WORKTREE_ROOT": str(tmp / "worktrees"),
            "AGENTBOARD_ADDR": f"127.0.0.1:{self.port}",
            "AGENTBOARD_API": "on",
        })
        self.client = Client([self.binary], self.env, str(repo))
        wait_for(lambda: self._get("/identity"), 30, "agentboard API")
        self.server_pid = int(self._tmux("display", "-p", "#{pid}"))

    def _tmux(self, *args: str) -> str:
        return subprocess.run(["tmux", *args], env=self.env, capture_output=True,
                              text=True, check=True).stdout.strip()

    def _get(self, path: str):
        with urllib.request.urlopen(f"http://127.0.0.1:{self.port}/api/v1{path}", timeout=5) as r:
            return json.load(r)

    def _post(self, path: str, body: dict):
        req = urllib.request.Request(f"http://127.0.0.1:{self.port}/api/v1{path}",
                                     data=json.dumps(body).encode(), method="POST",
                                     headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=60) as r:
            return json.load(r)

    def launch(self, n: int) -> None:
        for i in range(n):
            ticket = self._post("/tickets", {"title": f"bench agent {i + 1}"})
            sess = self._post(f"/tickets/{ticket['id']}/start", {"agent": "claude-code"})
            self.sessions.append(sess.get("id") or sess.get("session_id") or "")

    def accept_trust(self) -> None:
        for pane in self._tmux("list-panes", "-a", "-F", "#{pane_id}").split():
            if TRUST_DIALOG in self._tmux("capture-pane", "-p", "-t", pane):
                self._tmux("send-keys", "-t", pane, "Down")
                time.sleep(0.3)
                self._tmux("send-keys", "-t", pane, "Enter")

    def roots(self) -> list[int]:
        return [self.client.pid, self.server_pid]

    def stop(self, tmp: Path) -> None:
        subprocess.run(["tmux", "kill-server"], env=self.env, capture_output=True)
        self.client.close()
        shutil.rmtree(Path.home() / ".agentboard" / "projects" / self.repo.name, ignore_errors=True)
        cache = Path.home() / ".agentboard" / "cache"
        for sid in filter(None, self.sessions):
            for f in cache.glob(f"*{sid}*"):
                f.unlink(missing_ok=True)


class Herdr:
    name = "herdr"

    def __init__(self, binary: str):
        self.binary = binary

    def version(self) -> str:
        return subprocess.run([self.binary, "--version"], capture_output=True, text=True).stdout.strip()

    def _cli(self, *args: str) -> dict:
        out = subprocess.run([self.binary, "--session", self.session, *args], env=self.env,
                             capture_output=True, text=True, timeout=30)
        data = json.loads(out.stdout) if out.stdout.strip().startswith("{") else {}
        if "error" in data or out.returncode != 0:
            raise RuntimeError(f"herdr {' '.join(args)}: {out.stdout or out.stderr}")
        return data.get("result", data)

    def start(self, repo: Path, tmp: Path, env: dict) -> None:
        self.repo, self.env, self.panes = repo, dict(env), []
        self.session = f"abbench-{uuid.uuid4().hex[:6]}"
        self.client = Client([self.binary, "--session", self.session], self.env, str(repo))
        wait_for(lambda: self._cli("workspace", "list")["workspaces"], 30, "herdr server")
        table = proc_table()
        mine = descendants([self.client.pid], table)
        servers = [p for p in mine if table[p][1] == "herdr" and p != self.client.pid]
        self.server_pid = servers[0] if servers else self._find_server()

    def _find_server(self) -> int:
        sock = Path.home() / ".config" / "herdr" / "sessions" / self.session / "herdr.sock"
        out = subprocess.run(["ss", "-xlpn"], capture_output=True, text=True).stdout
        for line in out.splitlines():
            if str(sock) in line:
                m = re.search(r"pid=(\d+)", line)
                if m:
                    return int(m.group(1))
        raise RuntimeError("could not find the herdr server process")

    def launch(self, n: int) -> None:
        # Every agent gets a background tab and the focused tab keeps its idle
        # shell, so herdr's client draws no agent output — as agentboard's
        # client shows the board, not an agent window.
        ws = self._cli("workspace", "list")["workspaces"][0]["workspace_id"]
        for i in range(n):
            tab = self._cli("tab", "create", "--workspace", ws, "--cwd", str(self.repo),
                            "--label", f"agent {i + 1}", "--no-focus")
            pane = _find_key(tab, "pane_id")
            self._cli("pane", "run", pane, "claude")
            self.panes.append(pane)

    def accept_trust(self) -> None:
        for pane in self.panes:
            screen = subprocess.run([self.binary, "--session", self.session, "pane", "read", pane,
                                     "--source", "visible"], env=self.env,
                                    capture_output=True, text=True, timeout=30).stdout
            if TRUST_DIALOG in screen:
                self._cli("pane", "send-keys", pane, "Down")
                time.sleep(0.3)
                self._cli("pane", "send-keys", pane, "Enter")

    def roots(self) -> list[int]:
        return [self.client.pid, self.server_pid]

    def stop(self, tmp: Path) -> None:
        subprocess.run([self.binary, "--session", self.session, "server", "stop"],
                       env=self.env, capture_output=True, timeout=30)
        self.client.close()
        try:
            os.kill(self.server_pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        subprocess.run([self.binary, "session", "delete", self.session],
                       env=self.env, capture_output=True, timeout=30)


def _find_key(data, key: str):
    if isinstance(data, dict):
        if key in data:
            return data[key]
        for v in data.values():
            found = _find_key(v, key)
            if found is not None:
                return found
    if isinstance(data, list):
        for v in data:
            found = _find_key(v, key)
            if found is not None:
                return found
    return None


# --- measurement ------------------------------------------------------------

def stats(values: list[float]) -> dict:
    if not values:
        return {"median": None, "min": None, "max": None}
    return {"median": round(statistics.median(values), 1),
            "min": round(min(values), 1), "max": round(max(values), 1)}


def summarize(samples: list[dict], seconds: float) -> dict:
    """Reduce per-second snapshots to medians; CPU from first to last tick count."""
    mb = lambda s, b: s[b]["pss_kb"] / 1024
    first, last = samples[0], samples[-1]
    cpu = lambda b: round((last[b]["ticks"] - first[b]["ticks"]) / CLK_TCK / seconds * 100, 2)
    names: dict[str, list[float]] = {}
    for s in samples:
        for name, kb in s["tool_by_process_kb"].items():
            names.setdefault(name, []).append(kb / 1024)
    return {
        "tool_pss_mb": stats([mb(s, "tool") for s in samples]),
        "shell_pss_mb": stats([mb(s, "shell") for s in samples]),
        "agent_pss_mb": stats([mb(s, "agent") for s in samples]),
        "tool_cpu_pct": cpu("tool"),
        "processes": {b: last[b]["procs"] for b in ("tool", "shell", "agent")},
        "agents_running": min(s.get("agents_running", 0) for s in samples),
        "tool_by_process_mb": {k: round(statistics.median(v), 1) for k, v in sorted(names.items())},
    }


def run_one(tool, agents: int, workload: str, args, env: dict) -> dict:
    tmp = Path(tempfile.mkdtemp(prefix="abbench-"))
    started = time.monotonic()
    try:
        repo = make_repo(tmp)
        tool.start(repo, tmp, env)
        ready_s = time.monotonic() - started
        t0 = time.monotonic()
        tool.launch(agents)
        launch_s = time.monotonic() - t0
        if agents:
            got = wait_for(lambda: snapshot(tool.roots())["agents_running"] >= agents,
                           120, f"{agents} agent processes")
            assert got
        time.sleep(args.settle / 2)
        if workload == "claude":
            tool.accept_trust()
        time.sleep(args.settle / 2)
        alive = snapshot(tool.roots())["agents_running"]
        if alive < agents:
            raise RuntimeError(f"only {alive} of {agents} agents still running when sampling began")
        samples, t1 = [], time.monotonic()
        while time.monotonic() - t1 < args.duration:
            samples.append(snapshot(tool.roots()))
            time.sleep(args.interval)
        result = summarize(samples, time.monotonic() - t1)
        result.update({"tool": tool.name, "agents": agents, "workload": workload,
                       "samples": len(samples), "seconds_to_ready": round(ready_s, 2),
                       "seconds_to_launch": round(launch_s, 2),
                       "client_bytes_rendered": tool.client.bytes})
        return result
    finally:
        try:
            tool.stop(tmp)
        finally:
            time.sleep(1)
            kill_under(tmp)
            shutil.rmtree(tmp, ignore_errors=True)


def host_info() -> dict:
    cpu = next((l.split(":", 1)[1].strip() for l in Path("/proc/cpuinfo").read_text().splitlines()
                if l.startswith("model name")), platform.machine())
    mem = next(int(l.split()[1]) for l in Path("/proc/meminfo").read_text().splitlines()
               if l.startswith("MemTotal"))
    return {"os": platform.system(), "kernel": platform.release(), "arch": platform.machine(),
            "cpu": cpu, "cores": os.cpu_count(), "mem_total_mb": mem // 1024}


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("--tools", nargs="+", default=["agentboard", "herdr"], choices=["agentboard", "herdr"])
    p.add_argument("--agents", nargs="+", type=int, default=[0, 1, 5, 10])
    p.add_argument("--workload", choices=["stream", "claude"], default="stream")
    p.add_argument("--repeats", type=int, default=3)
    p.add_argument("--settle", type=float, default=20, help="seconds between launch and sampling")
    p.add_argument("--cooldown", type=float, default=3, help="seconds of rest between runs")
    p.add_argument("--duration", type=float, default=30, help="seconds of sampling per run")
    p.add_argument("--interval", type=float, default=1.0)
    p.add_argument("--lines-per-sec", type=float, default=20, help="stream workload output rate per agent")
    p.add_argument("--agentboard-bin", default=shutil.which("agentboard"))
    p.add_argument("--herdr-bin", default=shutil.which("herdr"))
    p.add_argument("--json-out")
    args = p.parse_args()

    if sys.platform != "linux":
        p.error("Linux only: memory is read from /proc")
    for t in args.tools:
        if not getattr(args, f"{t}_bin"):
            p.error(f"{t} not found; pass --{t}-bin")

    work = Path(tempfile.mkdtemp(prefix="abbench-bin-"))
    shell = work / "pane-shell"
    shell.write_text(PANE_SHELL)
    shell.chmod(0o755)
    env = dict(os.environ, SHELL=str(shell), BENCH_LINES_PER_SEC=str(args.lines_per_sec))
    if args.workload == "stream":
        (work / "bin").mkdir()
        fake = work / "bin" / "claude"
        fake.write_text(FAKE_AGENT)
        fake.chmod(0o755)
        env["PATH"] = f"{work / 'bin'}:{env['PATH']}"

    tools = {"agentboard": lambda: AgentBoard(args.agentboard_bin),
             "herdr": lambda: Herdr(args.herdr_bin)}
    meta = {}
    for t in args.tools:
        binary = getattr(args, f"{t}_bin")
        meta[t] = {"version": tools[t]().version(), "binary": binary,
                   "binary_bytes": os.path.getsize(binary)}
    if "agentboard" in meta:
        tmux = shutil.which("tmux")
        meta["agentboard"]["requires"] = {
            "tmux": {"version": subprocess.run(["tmux", "-V"], capture_output=True, text=True).stdout.strip(),
                     "binary_bytes": os.path.getsize(tmux)}}

    results = []
    try:
        for agents in args.agents:
            for rep in range(args.repeats):
                order = args.tools if rep % 2 == 0 else list(reversed(args.tools))
                for t in order:
                    print(f"[{t}] {args.workload} x{agents} run {rep + 1}/{args.repeats}", file=sys.stderr, flush=True)
                    try:
                        r = run_one(tools[t](), agents, args.workload, args, env)
                        r["repeat"] = rep + 1
                        print(f"    tool {r['tool_pss_mb']['median']} MB, cpu {r['tool_cpu_pct']}%",
                              file=sys.stderr, flush=True)
                    except Exception as e:
                        r = {"tool": t, "agents": agents, "workload": args.workload,
                             "repeat": rep + 1, "error": str(e)}
                        print(f"    error: {e}", file=sys.stderr, flush=True)
                    results.append(r)
                    time.sleep(args.cooldown)
    finally:
        shutil.rmtree(work, ignore_errors=True)

    summary = []
    for agents in args.agents:
        row = {"agents": agents, "workload": args.workload}
        for t in args.tools:
            ok = [r for r in results if r["tool"] == t and r["agents"] == agents and "error" not in r]
            row[t] = {
                "tool_pss_mb": round(statistics.median(r["tool_pss_mb"]["median"] for r in ok), 1) if ok else None,
                "tool_cpu_pct": round(statistics.median(r["tool_cpu_pct"] for r in ok), 2) if ok else None,
                "runs": len(ok),
            }
        if all(row.get(t, {}).get("tool_pss_mb") for t in ("agentboard", "herdr")):
            row["herdr_over_agentboard"] = round(row["herdr"]["tool_pss_mb"] / row["agentboard"]["tool_pss_mb"], 2)
        summary.append(row)

    payload = {
        "schema": SCHEMA,
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "host": host_info(),
        "config": {"workload": args.workload, "agents": args.agents, "repeats": args.repeats,
                   "settle_s": args.settle, "duration_s": args.duration, "interval_s": args.interval,
                   "lines_per_sec": args.lines_per_sec if args.workload == "stream" else None,
                   "terminal": f"{COLS}x{ROWS}", "metric": "PSS from /proc/<pid>/smaps_rollup"},
        "tools": meta,
        "summary": summary,
        "results": results,
    }
    text = json.dumps(payload, indent=2)
    if args.json_out:
        Path(args.json_out).write_text(text + "\n")
    print(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
