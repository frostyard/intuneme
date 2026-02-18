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

        // --- Bind to manager state ---
        this._managerSignals = [];

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

        this._brokerStatusItem.label.text = `Broker Proxy: ${
            this._manager.broker_running ? 'Running' : 'Stopped'
        }`;

        // Shell item only available when running and not transitioning
        this._shellItem.sensitive = running && !transitioning;
    }

    destroy() {
        for (const id of this._managerSignals)
            this._manager.disconnect(id);
        this._managerSignals = [];
        super.destroy();
    }
});
