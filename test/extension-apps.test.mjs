// Executable tests for the apps behavior of the GNOME extension.
//
// cmd/extension/*.js imports gi://GObject, gi://Gio, gi://GLib and the GNOME
// Shell modules, which plain Node does not have. test/extension-mocks/hooks.mjs
// resolves those specifiers to the minimal mocks in test/extension-mocks/,
// which record what the extension does, so these tests fail when a behavior
// goes away. node --check is not a test.
//
// Run with: node --test test/*.test.mjs (make verify-template).

import assert from 'node:assert/strict';
import { register } from 'node:module';
import { test } from 'node:test';

import * as GioMock from './extension-mocks/Gio.mjs';

register('./extension-mocks/hooks.mjs', import.meta.url);

// Imported after register() so the gi:// specifiers resolve to the mocks.
const { IntuneToggle } = await import('../cmd/extension/quickToggle.js');
const { ContainerManager } = await import('../cmd/extension/containerManager.js');

const APP_DEFAULT = {
    id: `msedge-${'a'.repeat(32)}-Default`,
    name: 'Outlook',
    profile: 'Default',
};
const APP_PROFILE_1 = {
    id: `msedge-${'b'.repeat(32)}-Profile_1`,
    name: 'Teams',
    profile: 'Profile_1',
};

/** Let every pending promise of the extension settle. */
function flush() {
    return new Promise((resolve) => setImmediate(resolve));
}

/** The labels of the items in the Apps submenu. */
function appLabels(toggle) {
    return toggle._appsItem.menu.items.map((item) => item.label.text);
}

/**
 * A stand-in for ContainerManager: it answers the property and method calls
 * IntuneToggle makes, and records them.
 */
function fakeManager({ running = false, transitioning = false, apps = [], listImpl = null } = {}) {
    return {
        container_running: running,
        broker_running: false,
        transitioning,
        error: false,
        listCalls: 0,
        openCalls: [],
        handlers: new Map(),
        nextHandlerId: 1,

        connect(name, callback) {
            const id = this.nextHandlerId++;
            if (!this.handlers.has(name))
                this.handlers.set(name, new Map());
            this.handlers.get(name).set(id, callback);
            return id;
        },

        disconnect(id) {
            for (const handlers of this.handlers.values())
                handlers.delete(id);
        },

        openShell() {},
        openEdge() {},
        openPortal() {},

        async listWebApps() {
            this.listCalls++;
            if (listImpl)
                return listImpl();
            return apps;
        },

        openWebApp(id) {
            this.openCalls.push(id);
        },
    };
}

/** A real ContainerManager on top of the mocked gi:// modules. */
function webAppManager() {
    GioMock.reset();
    return new ContainerManager();
}

/** Answer every command except `intuneme status` with the given result. */
function answerOpenWith(response) {
    GioMock.setResponder((argv) => {
        if (argv[1] === 'status')
            return { success: true, stdout: 'Container: running\n', stderr: '' };
        return response;
    });
}

test('menu open refreshes the apps only while the container runs and is not transitioning', async () => {
    const manager = fakeManager({ running: false });
    const toggle = new IntuneToggle(manager);

    toggle.menu.setOpen(true);
    await flush();
    assert.equal(manager.listCalls, 0, 'a stopped container must not list apps');
    assert.equal(toggle._appsItem.sensitive, false, 'the Apps submenu is disabled when stopped');

    manager.container_running = true;
    manager.transitioning = true;
    toggle.menu.setOpen(true);
    await flush();
    assert.equal(manager.listCalls, 0, 'a transitioning container must not list apps');

    manager.transitioning = false;
    toggle.menu.setOpen(true);
    await flush();
    assert.equal(manager.listCalls, 1, 'a running container lists apps on each menu open');

    toggle.destroy();
});

test('a closed menu does not refresh the apps', async () => {
    const manager = fakeManager({ running: true });
    const toggle = new IntuneToggle(manager);

    toggle.menu.setOpen(false);
    await flush();
    assert.equal(manager.listCalls, 0);

    toggle.destroy();
});

test('a failed command gives an empty list', async () => {
    const manager = webAppManager();
    answerOpenWith({ success: false, stdout: '', stderr: 'intuneme: no such file' });

    assert.deepEqual(await manager.listWebApps(), []);
    assert.deepEqual(
        GioMock.calls().at(-1),
        ['intuneme', 'open', 'app', '--list', '--json'],
        'the list is read with the CLI JSON output',
    );
    manager.destroy();
});

test('invalid JSON gives an empty list', async () => {
    const manager = webAppManager();
    answerOpenWith({ success: true, stdout: 'not json', stderr: '' });

    assert.deepEqual(await manager.listWebApps(), []);
    manager.destroy();
});

test('a non-array gives an empty list', async () => {
    const manager = webAppManager();
    answerOpenWith({ success: true, stdout: JSON.stringify({ apps: [APP_DEFAULT] }), stderr: '' });

    assert.deepEqual(await manager.listWebApps(), []);
    manager.destroy();
});

test('malformed entries are filtered out', async () => {
    const manager = webAppManager();
    answerOpenWith({
        success: true,
        stderr: '',
        stdout: JSON.stringify([
            APP_DEFAULT,
            null,
            'msedge-' + 'c'.repeat(32) + '-Default',
            { id: APP_PROFILE_1.id },
            { name: 'No id' },
            { id: 42, name: 'Number id' },
            { id: 'msedge-zzz-Default', name: 'Bad pattern' },
            { id: 'msedge-' + 'q'.repeat(32) + '-Default', name: 'Letters past p' },
            { id: 'org.gnome.Nautilus.desktop', name: 'Not a web app' },
            { id: `msedge-${'d'.repeat(32)}-Profile_0`, name: 'Bad profile' },
            { id: [APP_PROFILE_1.id], name: 'Array id' },
        ]),
    });

    const apps = await manager.listWebApps();
    assert.deepEqual(apps, [APP_DEFAULT], 'only the well-formed entry survives');
    manager.destroy();
});

test('activating an app item runs intuneme open app <id> and closes the menu', async () => {
    const manager = fakeManager({ running: true, apps: [APP_DEFAULT, APP_PROFILE_1] });
    const toggle = new IntuneToggle(manager);

    toggle.menu.setOpen(true);
    await flush();
    assert.deepEqual(appLabels(toggle), ['Outlook', 'Teams']);

    const item = toggle._appsItem.menu.items[1];
    item.emit('activate');

    await flush();
    assert.deepEqual(manager.openCalls, [APP_PROFILE_1.id], 'the exact ID reaches openWebApp');
    assert.equal(toggle.menu.closed, 1, 'the menu closes on activation');

    toggle.destroy();
});

test('openWebApp runs the CLI with the ID as 1 argument', async () => {
    const manager = webAppManager();
    answerOpenWith({ success: true, stdout: '', stderr: '' });

    manager.openWebApp(APP_PROFILE_1.id);
    await flush();

    assert.deepEqual(
        GioMock.calls().at(-1),
        ['intuneme', 'open', 'app', APP_PROFILE_1.id],
        'the ID is 1 argv element, not a shell string',
    );
    manager.destroy();
});

test('openWebApp refuses an ID that does not match the pattern', async () => {
    const manager = webAppManager();
    answerOpenWith({ success: true, stdout: '', stderr: '' });

    manager.openWebApp('../org.gnome.Nautilus');
    await flush();

    const launched = GioMock.calls().filter((argv) => argv[1] === 'open');
    assert.deepEqual(launched, [], 'a malformed ID starts no command');
    manager.destroy();
});

test('open edge and open portal still pass their subcommand', async () => {
    const manager = webAppManager();
    answerOpenWith({ success: true, stdout: '', stderr: '' });

    manager.openEdge();
    manager.openPortal();
    await flush();

    const launched = GioMock.calls().filter((argv) => argv[1] === 'open');
    assert.deepEqual(launched, [
        ['intuneme', 'open', 'edge'],
        ['intuneme', 'open', 'portal'],
    ]);
    manager.destroy();
});

test('an empty list shows the non-reactive "No web apps installed" item', async () => {
    const manager = fakeManager({ running: true, apps: [] });
    const toggle = new IntuneToggle(manager);

    toggle.menu.setOpen(true);
    await flush();

    const items = toggle._appsItem.menu.items;
    assert.equal(items.length, 1);
    assert.equal(items[0].label.text, 'No web apps installed');
    assert.equal(items[0].reactive, false);

    toggle.destroy();
});

test('a result that arrives after a newer refresh does not change the submenu', async () => {
    let resolveFirst;
    let resolveSecond;
    const first = new Promise((resolve) => { resolveFirst = resolve; });
    const second = new Promise((resolve) => { resolveSecond = resolve; });
    const results = [first, second];
    const manager = fakeManager({ running: true, listImpl: () => results.shift() });
    const toggle = new IntuneToggle(manager);

    toggle._refreshApps();
    toggle._refreshApps();

    resolveSecond([APP_PROFILE_1]);
    await flush();
    assert.deepEqual(appLabels(toggle), ['Teams']);

    // The older refresh answers last: it must be dropped.
    resolveFirst([APP_DEFAULT]);
    await flush();
    assert.deepEqual(appLabels(toggle), ['Teams'], 'a stale result must not replace the list');

    toggle.destroy();
});

test('a result that arrives after destroy does not change the submenu', async () => {
    let resolveList;
    const pending = new Promise((resolve) => { resolveList = resolve; });
    const manager = fakeManager({ running: true, listImpl: () => pending });
    const toggle = new IntuneToggle(manager);

    toggle._refreshApps();
    toggle.destroy();

    resolveList([APP_DEFAULT]);
    await flush();

    assert.deepEqual(toggle._appsItem.menu.items, [], 'a destroyed toggle fills no submenu');
    assert.equal(toggle._appsItem.menu.removeAllCalls, 0, 'a destroyed toggle touches no menu');
});
