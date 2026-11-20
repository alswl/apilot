package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/alswl/apilot/core/pkg/common"
)

func VerifyWorkbenchObsidianCLIWrite(ctx context.Context, c common.WorkbenchConnection) error {
	cli, err := openWorkbenchObsidianCLI(ctx, c)
	if err != nil {
		return err
	}
	result, err := cli.run(ctx, "eval", `code=typeof app.vault.process === "function" && typeof require === "function" && typeof require("crypto").createHash === "function"`)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(result)) != "=> true" {
		return common.Err("CONFLICT", "Obsidian CLI does not support conditional writes")
	}
	return nil
}

type workbenchObsidianPatch struct {
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Insert []byte `json:"insert"`
}

// Send only the changed bytes. Even a large note then fits in a CLI argument.
func workbenchObsidianNotePatch(before, after []byte) workbenchObsidianPatch {
	start := 0
	for start < min(len(before), len(after)) && before[start] == after[start] {
		start++
	}
	end, afterEnd := len(before), len(after)
	for end > start && afterEnd > start && before[end-1] == after[afterEnd-1] {
		end--
		afterEnd--
	}
	return workbenchObsidianPatch{Start: start, End: end, Insert: after[start:afterEnd]}
}

// PrepareWorkbenchObsidianCLIWrite verifies the vault before a write receipt is staged.
func PrepareWorkbenchObsidianCLIWrite(ctx context.Context, c common.WorkbenchConnection, path string, before, after []byte) (func() error, error) {
	if !workbenchVisiblePath(path) || !strings.EqualFold(filepath.Ext(path), ".md") ||
		(filepath.Clean(c.Scope) != "." && !strings.HasPrefix(path, filepath.ToSlash(c.Scope)+"/")) {
		return nil, common.Err("INVALID_ARGUMENT", "Choose a Markdown note within the connection scope")
	}
	if !utf8.Valid(before) || !utf8.Valid(after) {
		return nil, common.Err("CONFLICT", "Obsidian CLI writes require valid UTF-8 Markdown")
	}
	cli, err := openWorkbenchObsidianCLI(ctx, c)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(before)
	input, _ := json.Marshal(struct {
		Vault string `json:"vault"`
		Path  string `json:"path"`
		Hash  string `json:"hash"`
		workbenchObsidianPatch
	}{cli.basePath, path, hex.EncodeToString(digest[:]), workbenchObsidianNotePatch(before, after)})
	script := `(async () => {
		const input = ` + string(input) + `;
		const vault = app.vault;
		if (vault.adapter.getBasePath() !== input.vault) throw new Error("Unexpected vault");
		const file = vault.getFileByPath(input.path);
		if (!file) return "stale";
		const { createHash } = require("crypto");
		const { Buffer } = require("buffer");
		let stale = false;
		try {
			await vault.process(file, current => {
				const data = Buffer.from(current, "utf8");
				if (createHash("sha256").update(data).digest("hex") !== input.hash) {
					stale = true;
					throw new Error("Note changed");
				}
				return Buffer.concat([
					data.subarray(0, input.start),
					Buffer.from(input.insert || "", "base64"),
					data.subarray(input.end)
				]).toString("utf8");
			});
		} catch (error) {
			if (stale) return "stale";
			throw error;
		}
		return "written";
	})()`
	if len(script) > 64<<10 {
		return nil, common.Err("CONFLICT", "Obsidian CLI write exceeds the command size limit")
	}
	return func() error {
		result, err := cli.run(ctx, "eval", "code="+script)
		if err != nil {
			return err
		}
		switch strings.TrimSpace(string(result)) {
		case "=> written":
			return nil
		case "=> stale":
			return common.Err("STALE_REVISION", "Obsidian note changed before writing")
		default:
			return common.Err("CONFLICT", "Obsidian CLI did not confirm the note update")
		}
	}, nil
}
