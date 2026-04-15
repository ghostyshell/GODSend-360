import { existsSync } from "fs";
import { join } from "path";

// Mirrors the PreToolUse hook in .claude/settings.json:
// injects graphify knowledge-graph context before file searches so the agent
// reads GRAPH_REPORT.md instead of trawling raw files when a graph exists.
export const GraphifyContextPlugin = async ({ directory }) => {
  return {
    "tool.execute.before": async (input) => {
      if (input.tool === "glob" || input.tool === "grep") {
        if (existsSync(join(directory, "graphify-out", "graph.json"))) {
          throw new Error(
            "graphify: Knowledge graph exists. Read graphify-out/GRAPH_REPORT.md for god nodes and community structure before searching raw files."
          );
        }
      }
    },
  };
};
