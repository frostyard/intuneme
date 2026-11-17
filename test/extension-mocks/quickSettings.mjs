// Minimal stand-in for resource:///org/gnome/shell/ui/quickSettings.js: the
// QuickMenuToggle base class of the Intune toggle, with its popup menu and
// signal API.

import { PopupMenu } from './popupMenu.mjs';

export class QuickMenuToggle {
    _init(params = {}) {
        this.menu = new PopupMenu();
        this.title = params.title;
        this.subtitle = params.subtitle;
        this.iconName = params.iconName;
        this.toggleMode = params.toggleMode;
        this.checked = false;
        this.reactive = true;
        this.destroyed = false;
        this._handlers = new Map();
        this._nextHandlerId = 1;
    }

    connect(name, callback) {
        const id = this._nextHandlerId++;
        if (!this._handlers.has(name))
            this._handlers.set(name, new Map());
        this._handlers.get(name).set(id, callback);
        return id;
    }

    disconnect(id) {
        for (const handlers of this._handlers.values())
            handlers.delete(id);
    }

    emit(name, ...args) {
        const handlers = this._handlers.get(name);
        if (!handlers)
            return;
        for (const callback of [...handlers.values()])
            callback(this, ...args);
    }

    destroy() {
        this.destroyed = true;
    }
}

export class SystemIndicator {}
