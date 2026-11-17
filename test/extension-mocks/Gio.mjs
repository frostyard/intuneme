// Minimal stand-in for gi://Gio: Gio.Subprocess records every argv and answers
// from a responder the test sets, and the D-Bus watch is a no-op.
//
// Use setResponder() to decide what 1 command returns, calls() to read the
// argv of every command the extension started, and reset() between tests.

const STATUS_OUTPUT = 'Container: stopped\nBroker proxy: stopped\n';

let responder = defaultResponder;
const recorded = [];

function defaultResponder(argv) {
    if (argv[1] === 'status')
        return { success: true, stdout: STATUS_OUTPUT, stderr: '' };
    return { success: true, stdout: '', stderr: '' };
}

/** Set the function that answers a command: (argv) => {success, stdout, stderr}. */
export function setResponder(fn) {
    responder = fn ?? defaultResponder;
}

/** The argv of every command started since the last reset(). */
export function calls() {
    return recorded;
}

/** Forget the recorded commands and go back to the default answers. */
export function reset() {
    recorded.length = 0;
    responder = defaultResponder;
}

class Subprocess {
    constructor(argv, flags) {
        this.argv = argv;
        this.flags = flags;
        this._response = { success: true, stdout: '', stderr: '' };
    }

    static new(argv, flags) {
        recorded.push([...argv]);
        return new Subprocess(argv, flags);
    }

    // execCommand() awaits this and then reads get_successful().
    communicate_utf8_async() {
        this._response = responder(this.argv) ?? { success: true, stdout: '', stderr: '' };
        return Promise.resolve([
            this._response.stdout ?? '',
            this._response.stderr ?? '',
        ]);
    }

    get_successful() {
        return this._response.success ?? true;
    }

    wait_async() {}

    wait_finish() {}
}

const DBus = {
    system: {
        signal_subscribe: () => 1,
        signal_unsubscribe: () => {},
    },
};

export default {
    Subprocess,
    SubprocessFlags: { NONE: 0, STDOUT_PIPE: 1, STDERR_PIPE: 2 },
    DBus,
    DBusSignalFlags: { NONE: 0 },
    _promisify: () => {},
};
