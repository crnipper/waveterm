local function update()
    local pane = os.getenv("TMUX_PANE")
    if not pane then return end
    local cwd = tostring(cx.active.current.cwd)
    ya.async(function()
        Command("tmux"):arg({ "set-option", "-p", "-t", pane, "@wave-yazi-cwd", cwd }):status()
    end)
end

return {
    setup = function()
        ps.sub("cd", update)
        ps.sub("tab", update)
        ps.sub("load", update)
    end,
}
