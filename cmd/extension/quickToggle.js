import GObject from 'gi://GObject';
import * as QuickSettings from 'resource:///org/gnome/shell/ui/quickSettings.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

export const IntuneToggle = GObject.registerClass(
class IntuneToggle extends QuickSettings.QuickMenuToggle {
    _init(manager) {
        super._init({
            title: 'Intune',
            subtitle: 'Stopped',
            iconName: 'computer-symbolic',
            toggleMode: true,
        });

        this._manager = manager;

        // --- Popup menu ---
        this.menu.setHeader('computer-symbolic', 'Intune Container');

        // Status section
        this._statusSection = new PopupMenu.PopupMenuSection();

        this._containerStatusItem = new PopupMenu.PopupMenuItem('Container: Stopped', {
            reactive: false,
        });
        this._statusSection.addMenuItem(this._containerStatusItem);

        this._brokerStatusItem = new PopupMenu.PopupMenuItem('Broker Proxy: Unknown', {
            reactive: false,
        });
        this._statusSection.addMenuItem(this._brokerStatusItem);

        this.menu.addMenuItem(this._statusSection);
        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Open Shell action
        this._shellItem = this.menu.addAction('Open Shell', () => {
            this.menu.close();
            this._manager.openShell();
        });
        this._shellItem.sensitive = false;

        // Open Edge action
        this._edgeItem = this.menu.addAction('Open Edge', () => {
            this.menu.close();
            this._manager.openEdge();
        });
        this._edgeItem.sensitive = false;

        // Open Intune Portal action
        this._portalItem = this.menu.addAction('Open Intune Portal', () => {
            this.menu.close();
            this._manager.openPortal();
        });
        this._portalItem.sensitive = false;

        // Edge web apps submenu, filled from the container when the menu opens
        this._appsItem = new PopupMenu.PopupSubMenuMenuItem('Apps');
        this._appsItem.sensitive = false;
        this.menu.addMenuItem(this._appsItem);
        this._appsGeneration = 0;
        this._destroyed = false;

        // --- Bind to manager state ---
        this._managerSignals = [];

        // Reload the app list each time the menu opens.
        this._openStateId = this.menu.connect('open-state-changed', (_menu, isOpen) => {
            if (!isOpen)
                return;
            if (!this._manager.container_running || this._manager.transitioning)
                return;
            if (this._manager.identity === 'himmelblau-experimental')
                return;
            this._refreshApps();
        });

        this._managerSignals.push(
            this._manager.connect('notify::container-running', () => this._sync()),
        );
        this._managerSignals.push(
            this._manager.connect('notify::broker-running', () => this._sync()),
        );
        this._managerSignals.push(
            this._manager.connect('notify::transitioning', () => this._sync()),
        );
        this._managerSignals.push(
            this._manager.connect('notify::error', () => this._sync()),
        );
        this._managerSignals.push(
            this._manager.connect('notify::identity', () => this._sync()),
        );

        // Handle toggle clicks
        this.connect('clicked', () => {
            if (this._manager.transitioning)
                return;

            if (this._manager.container_running)
                this._manager.stop();
            else
                this._manager.start();
        });

        // Initial sync
        this._sync();
    }

    _sync() {
        const running = this._manager.container_running;
        const transitioning = this._manager.transitioning;
        const error = this._manager.error;
        const himmelblau = this._manager.identity === 'himmelblau-experimental';

        this.title = himmelblau ? 'Himmelblau' : 'Intune';
        this.menu.setHeader('computer-symbolic', himmelblau ? 'Himmelblau Container' : 'Intune Container');

        // Toggle state
        this.checked = running;
        this.reactive = !transitioning;

        // Subtitle
        if (error)
            this.subtitle = 'Error';
        else if (transitioning)
            this.subtitle = running ? 'Stopping\u2026' : 'Starting\u2026';
        else
            this.subtitle = running ? 'Running' : 'Stopped';

        // Menu items
        this._containerStatusItem.label.text = `Container: ${
            error ? 'Error'
                : transitioning
                    ? (running ? 'Stopping\u2026' : 'Starting\u2026')
                    : (running ? 'Running' : 'Stopped')
        }`;

        this._brokerStatusItem.label.text = `${himmelblau ? 'Himmelblau ' : ''}Broker Proxy: ${
            this._manager.broker_running ? 'Running' : 'Stopped'
        }`;

        this._portalItem.label.text = himmelblau ? 'Himmelblau Sign-in' : 'Open Intune Portal';

        // Shell/Edge/enrollment/Apps items only available when running and not transitioning
        this._shellItem.sensitive = running && !transitioning;
        this._edgeItem.sensitive = running && !transitioning;
        this._edgeItem.actor.visible = !himmelblau;
        this._portalItem.sensitive = running && !transitioning;
        this._appsItem.sensitive = running && !transitioning && !himmelblau;
        this._appsItem.actor.visible = !himmelblau;
        if (!running || himmelblau)
            this._appsItem.menu.close();
    }

    /**
     * Reload the Apps submenu from `intuneme open app --list --json`.
     * A newer call, or a destroyed toggle, discards the result.
     */
    async _refreshApps() {
        const generation = ++this._appsGeneration;

        let apps = [];
        try {
            apps = await this._manager.listWebApps();
        } catch (e) {
            console.warn(`[intuneme] Failed to list web apps: ${e.message}`);
            return;
        }

        if (generation !== this._appsGeneration || this._destroyed)
            return;

        this._appsItem.menu.removeAll();

        if (apps.length === 0) {
            const empty = new PopupMenu.PopupMenuItem('No web apps installed', {
                reactive: false,
            });
            this._appsItem.menu.addMenuItem(empty);
            return;
        }

        for (const app of apps) {
            const item = new PopupMenu.PopupMenuItem(app.name);
            item.connect('activate', () => {
                this.menu.close();
                this._manager.openWebApp(app.id);
            });
            this._appsItem.menu.addMenuItem(item);
        }
    }

    destroy() {
        // _destroyed is what drops a list that is still in flight; the menu
        // signal is disconnected, so no newer refresh can start.
        this._destroyed = true;
        if (this._openStateId) {
            this.menu.disconnect(this._openStateId);
            this._openStateId = null;
        }
        for (const id of this._managerSignals)
            this._manager.disconnect(id);
        this._managerSignals = [];
        super.destroy();
    }
});
