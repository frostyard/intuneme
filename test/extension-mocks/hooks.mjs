// Resolve the GNOME Shell and GObject specifiers of the extension to the mocks
// in this directory, so node:test can import cmd/extension/*.js without GJS.
// test/extension-apps.test.mjs registers this module with module.register().

const MOCKS = {
    'gi://GObject': './GObject.mjs',
    'gi://Gio': './Gio.mjs',
    'gi://GLib': './GLib.mjs',
    'resource:///org/gnome/shell/ui/quickSettings.js': './quickSettings.mjs',
    'resource:///org/gnome/shell/ui/popupMenu.js': './popupMenu.mjs',
};

export async function resolve(specifier, context, nextResolve) {
    const mock = MOCKS[specifier];
    if (mock)
        return { url: new URL(mock, import.meta.url).href, shortCircuit: true };
    return nextResolve(specifier, context);
}
