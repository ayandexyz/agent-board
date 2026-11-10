import importlib.util
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("bench", Path(__file__).with_name("bench-memory.py"))
bench = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bench)


class ClassifyTest(unittest.TestCase):
    def test_agent_subtrees_and_shells_are_separated_from_the_tool(self):
        table = {
            1: (0, "tmux: server", 0),
            2: (1, "agentboard", 0),
            3: (1, "bash", 0),
            4: (3, "claude", 0),
            5: (4, "node", 0),
            6: (4, "bash", 0),
            7: (2, "git", 0),
        }
        kinds = bench.classify(bench.descendants([1], table), table)
        self.assertEqual(kinds, {1: "tool", 2: "tool", 3: "shell", 4: "agent",
                                 5: "agent", 6: "agent", 7: "tool"})

    def test_descendants_ignores_roots_that_are_gone(self):
        table = {1: (0, "herdr", 0), 2: (1, "herdr", 0)}
        self.assertEqual(bench.descendants([1, 99], table), {1, 2})


class SummarizeTest(unittest.TestCase):
    def snap(self, tool_kb, ticks):
        empty = {"pss_kb": 0, "procs": 0, "ticks": 0}
        return {"tool": {"pss_kb": tool_kb, "procs": 2, "ticks": ticks},
                "shell": dict(empty), "agent": dict(empty),
                "tool_by_process_kb": {"agentboard": tool_kb}}

    def test_median_memory_and_cpu_over_the_window(self):
        hz = bench.CLK_TCK
        samples = [self.snap(10240, 0), self.snap(30720, hz // 2), self.snap(20480, hz)]
        got = bench.summarize(samples, seconds=10)
        self.assertEqual(got["tool_pss_mb"], {"median": 20.0, "min": 10.0, "max": 30.0})
        self.assertEqual(got["tool_cpu_pct"], 10.0)
        self.assertEqual(got["tool_by_process_mb"], {"agentboard": 20.0})


if __name__ == "__main__":
    unittest.main()
