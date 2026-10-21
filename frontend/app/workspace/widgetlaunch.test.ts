// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0
import { assert, test } from "vitest";
import { getWidgetBlockDef } from "./widgetlaunch";
const widget: WidgetConfigType = {
    inheritcwd: true,
    blockdef: { meta: { connection: "user@vm", "cmd:env": { WAVE_TMUX: "1" } } },
};
const source = { oid: "source-id", meta: { view: "term", connection: "user@vm", "cmd:cwd": "/repo/a b" } } as Block;
test("captures source cwd and preserves environment without mutation", () => {
    const result = getWidgetBlockDef(widget, source);
    assert.equal(result.meta["cmd:cwd"], "/repo/a b");
    assert.deepEqual(result.meta["cmd:env"], {
        WAVE_TMUX: "1",
        WAVE_BROWSER_SOURCE_BLOCK: "source-id",
        WAVE_BROWSER_SOURCE_CWD: "/repo/a b",
    });
    assert.notProperty(widget.blockdef.meta["cmd:env"], "WAVE_BROWSER_SOURCE_BLOCK");
});
test("keeps defaults for opt-out, no selection, non-terminal and other hosts", () => {
    assert.strictEqual(getWidgetBlockDef({ ...widget, inheritcwd: false }, source), widget.blockdef);
    assert.strictEqual(getWidgetBlockDef(widget, null), widget.blockdef);
    assert.strictEqual(getWidgetBlockDef(widget, { ...source, meta: { view: "preview" } }), widget.blockdef);
    assert.strictEqual(
        getWidgetBlockDef(widget, { ...source, meta: { ...source.meta, connection: "other@vm" } }),
        widget.blockdef
    );
});
test("passes source ID when tmux prevents Wave cwd tracking", () => {
    const result = getWidgetBlockDef(widget, { ...source, meta: { view: "term", connection: "user@vm" } });
    assert.equal(result.meta["cmd:env"].WAVE_BROWSER_SOURCE_BLOCK, "source-id");
    assert.equal(result.meta["cmd:env"].WAVE_BROWSER_SOURCE_CWD, "");
});
