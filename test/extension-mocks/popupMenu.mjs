// Minimal stand-in for resource:///org/gnome/shell/ui/popupMenu.js: the parts of
// a popup menu that quickToggle.js builds and reads, with test helpers to open
// the menu and to activate an item.

class SignalSource {
    connect(name, callback) {
        if (!this._handlers)
            this._handlers = new Map();
        if (!this._nextHandlerId)
            this._nextHandlerId = 1;
        const id = this._nextHandlerId++;
        if (!this._handlers.has(name))
            this._handlers.set(name, new Map());
        this._handlers.get(name).set(id, callback);
        return id;
    }

    disconnect(id) {
        if (!this._handlers)
            return;
        for (const handlers of this._handlers.values())
            handlers.delete(id);
    }

    emit(name, ...args) {
        const handlers = this._handlers?.get(name);
        if (!handlers)
            return;
        for (const callback of [...handlers.values()])
            callback(this, ...args);
    }
}

export class PopupBaseMenuItem extends SignalSource {
    constructor(label, params = {}) {
        super();
        this.label = { text: label };
        this.reactive = params.reactive ?? true;
    }

    destroy() {
        this.destroyed = true;
    }
}

export class PopupMenuItem extends PopupBaseMenuItem {}

export class PopupSeparatorMenuItem extends PopupBaseMenuItem {
    constructor() {
        super('');
    }
}

export class PopupMenuSection extends SignalSource {
    constructor() {
        super();
        this.items = [];
    }

    addMenuItem(item) {
        this.items.push(item);
        return item;
    }

    removeAll() {
        this.items = [];
    }
}

export class PopupMenu extends SignalSource {
    constructor() {
        super();
        this.items = [];
        this.closed = 0;
        this.open = false;
        this.removeAllCalls = 0;
    }

    setHeader(icon, title) {
        this.header = { icon, title };
    }

    addMenuItem(item) {
        this.items.push(item);
        return item;
    }

    addAction(label, callback) {
        const item = new PopupMenuItem(label);
        item.connect('activate', () => callback());
        this.items.push(item);
        return item;
    }

    removeAll() {
        this.items = [];
        this.removeAllCalls++;
    }

    close() {
        this.closed++;
        this.open = false;
        this.emit('open-state-changed', false);
    }

    // Test helper: what GNOME Shell emits when the menu opens or closes.
    setOpen(value) {
        this.open = value;
        this.emit('open-state-changed', value);
    }
}

export class PopupSubMenuMenuItem extends PopupBaseMenuItem {
    constructor(label) {
        super(label);
        this.menu = new PopupMenu();
    }
}
