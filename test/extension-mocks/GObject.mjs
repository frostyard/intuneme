// Minimal stand-in for gi://GObject: enough for GObject.registerClass,
// GObject.Object, ParamSpec and signal handling as the extension uses them.
// A registered class is wrapped so that `new Klass(...)` calls `_init(...)`,
// the way GJS constructs a GObject.

class Emitter {
    constructor() {
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

    notify(name) {
        this.emit(`notify::${name}`);
    }
}

class GObjectObject extends Emitter {
    _init() {}
}

function registerClass(...args) {
    const klass = args[args.length - 1];
    return class extends klass {
        constructor(...initArgs) {
            super();
            if (typeof this._init === 'function')
                this._init(...initArgs);
        }
    };
}

export default {
    registerClass,
    Object: GObjectObject,
    ParamSpec: { boolean: () => ({}) },
    ParamFlags: { READABLE: 1, WRITABLE: 2, READWRITE: 3 },
};
