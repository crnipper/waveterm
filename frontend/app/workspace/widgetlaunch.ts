// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

export function getWidgetBlockDef(widget: WidgetConfigType, source: Block): BlockDef {
    if (!widget.inheritcwd || source?.meta?.view != "term") return widget.blockdef;
    const meta = widget.blockdef.meta;
    // A remote path is meaningful only on its originating connection.
    if (meta.connection != source.meta.connection) return widget.blockdef;
    return {
        ...widget.blockdef,
        meta: {
            ...meta,
            "cmd:cwd": source.meta["cmd:cwd"] ?? meta["cmd:cwd"],
            "cmd:env": {
                ...meta["cmd:env"],
                WAVE_BROWSER_SOURCE_BLOCK: source.oid,
                WAVE_BROWSER_SOURCE_CWD: source.meta["cmd:cwd"] ?? "",
            },
        },
    };
}
