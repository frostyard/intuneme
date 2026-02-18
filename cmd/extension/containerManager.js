import GObject from 'gi://GObject';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';

const MACHINE_NAME = 'intuneme';
const POLL_INTERVAL_SECONDS = 5;
const INTUNEME_BIN = 'intuneme';

// Terminal emulators to try, in order of preference.
const TERMINALS = ['ptyxis', 'kgx', 'gnome-terminal', 'xterm'];

Gio._promisify(Gio.Subprocess.prototype, 'communicate_utf8_async');

/**
 * Run a command and return [success, stdout, stderr].
 */
async function execCommand(argv) {
    try {
        const proc = Gio.Subprocess.new(
            argv,
            Gio.SubprocessFlags.STDOUT_PIPE | Gio.SubprocessFlags.STDERR_PIPE,
        );
        const [stdout, stderr] = await proc.communicate_utf8_async(null, null);
        return [proc.get_successful(), stdout?.trim() ?? '', stderr?.trim() ?? ''];
    } catch (e) {
        return [false, '', e.message];
    }
}

/**
 * Find a terminal emulator on $PATH.
 * Checks $TERMINAL env var first, then a built-in list.
 */
function findTerminal() {
    const envTerminal = GLib.getenv('TERMINAL');
    if (envTerminal && GLib.find_program_in_path(envTerminal))
        return envTerminal;

    for (const term of TERMINALS) {
        if (GLib.find_program_in_path(term))
            return term;
    }
    return null;
}

export const ContainerManager = GObject.registerClass({
    Properties: {
        'container-running': GObject.ParamSpec.boolean(
            'container-running', '', '',
            GObject.ParamFlags.READABLE,
            false,
        ),
        'broker-running': GObject.ParamSpec.boolean(
            'broker-running', '', '',
            GObject.ParamFlags.READABLE,
            false,
        ),
        'transitioning': GObject.ParamSpec.boolean(
            'transitioning', '', '',
            GObject.ParamFlags.READABLE,
            false,
        ),
    },
}, class ContainerManager extends GObject.Object {
    _init() {
        super._init();

        this._containerRunning = false;
        this._brokerRunning = false;
        this._transitioning = false;

        this._setupDBusWatch();
        this._startPolling();
        // Do an immediate status check
        this._pollStatus();
    }

    get container_running() {
        return this._containerRunning;
    }

    get broker_running() {
        return this._brokerRunning;
    }

    get transitioning() {
        return this._transitioning;
    }

    _setContainerRunning(value) {
        if (this._containerRunning !== value) {
            this._containerRunning = value;
            this.notify('container-running');
        }
    }

    _setBrokerRunning(value) {
        if (this._brokerRunning !== value) {
            this._brokerRunning = value;
            this.notify('broker-running');
        }
    }

    _setTransitioning(value) {
        if (this._transitioning !== value) {
            this._transitioning = value;
            this.notify('transitioning');
        }
    }

    /**
     * Subscribe to MachineNew / MachineRemoved signals on the system bus.
     */
    _setupDBusWatch() {
        try {
            this._systemBus = Gio.DBus.system;
            this._machineNewId = this._systemBus.signal_subscribe(
                'org.freedesktop.machine1',
                'org.freedesktop.machine1.Manager',
                'MachineNew',
                '/org/freedesktop/machine1',
                null,
                Gio.DBusSignalFlags.NONE,
                (_conn, _sender, _path, _iface, _signal, params) => {
                    const name = params.get_child_value(0).get_string()[0];
                    if (name === MACHINE_NAME) {
                        this._setContainerRunning(true);
                        this._setTransitioning(false);
                    }
                },
            );
            this._machineRemovedId = this._systemBus.signal_subscribe(
                'org.freedesktop.machine1',
                'org.freedesktop.machine1.Manager',
                'MachineRemoved',
                '/org/freedesktop/machine1',
                null,
                Gio.DBusSignalFlags.NONE,
                (_conn, _sender, _path, _iface, _signal, params) => {
                    const name = params.get_child_value(0).get_string()[0];
                    if (name === MACHINE_NAME) {
                        this._setContainerRunning(false);
                        this._setBrokerRunning(false);
                        this._setTransitioning(false);
                    }
                },
            );
        } catch (e) {
            console.warn(`[intuneme] D-Bus signal watch failed, using polling only: ${e.message}`);
        }
    }

    /**
     * Poll `intuneme status` every POLL_INTERVAL_SECONDS.
     */
    _startPolling() {
        this._pollSourceId = GLib.timeout_add_seconds(
            GLib.PRIORITY_DEFAULT,
            POLL_INTERVAL_SECONDS,
            () => {
                this._pollStatus();
                return GLib.SOURCE_CONTINUE;
            },
        );
    }

    async _pollStatus() {
        const [ok, stdout] = await execCommand([INTUNEME_BIN, 'status']);
        if (!ok)
            return;

        const containerMatch = stdout.match(/^Container:\s+(\w+)/m);
        if (containerMatch) {
            const running = containerMatch[1] === 'running';
            if (!this._transitioning)
                this._setContainerRunning(running);
        }

        const brokerMatch = stdout.match(/^Broker proxy:\s+(\w+)/m);
        this._setBrokerRunning(brokerMatch ? brokerMatch[1] === 'running' : false);
    }

    /**
     * Start the container via pkexec.
     */
    async start() {
        if (this._containerRunning || this._transitioning)
            return;

        this._setTransitioning(true);
        const [ok, , stderr] = await execCommand(['pkexec', INTUNEME_BIN, 'start']);
        if (!ok) {
            console.warn(`[intuneme] start failed: ${stderr}`);
            this._setTransitioning(false);
            // Poll to reconcile state
            this._pollStatus();
        }
        // On success, D-Bus MachineNew signal will flip state
    }

    /**
     * Stop the container.
     */
    async stop() {
        if (!this._containerRunning || this._transitioning)
            return;

        this._setTransitioning(true);
        const [ok, , stderr] = await execCommand([INTUNEME_BIN, 'stop']);
        if (!ok) {
            console.warn(`[intuneme] stop failed: ${stderr}`);
            this._setTransitioning(false);
            this._pollStatus();
        }
        // On success, D-Bus MachineRemoved signal will flip state
    }

    /**
     * Open a terminal with `intuneme shell`.
     */
    openShell() {
        const terminal = findTerminal();
        if (!terminal) {
            console.error('[intuneme] No terminal emulator found');
            return;
        }

        try {
            // Most terminals use `-- command args` to run a command
            const proc = Gio.Subprocess.new(
                [terminal, '--', INTUNEME_BIN, 'shell'],
                Gio.SubprocessFlags.NONE,
            );
            proc.wait_async(null, null);
        } catch (e) {
            console.error(`[intuneme] Failed to launch terminal: ${e.message}`);
        }
    }

    destroy() {
        if (this._pollSourceId) {
            GLib.source_remove(this._pollSourceId);
            this._pollSourceId = null;
        }
        if (this._systemBus) {
            if (this._machineNewId)
                this._systemBus.signal_unsubscribe(this._machineNewId);
            if (this._machineRemovedId)
                this._systemBus.signal_unsubscribe(this._machineRemovedId);
        }
    }
});
