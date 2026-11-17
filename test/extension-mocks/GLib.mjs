// Minimal stand-in for gi://GLib: no timer ever fires, so a test controls every
// call the extension makes.

let nextSourceId = 1;

export default {
    PRIORITY_DEFAULT: 0,
    SOURCE_CONTINUE: true,
    SOURCE_REMOVE: false,

    get_home_dir: () => '/home/tester',
    getenv: () => null,
    find_program_in_path: () => null,
    path_get_basename: (path) => path.split('/').pop(),

    // Never schedules the callback: the test drives the extension itself.
    timeout_add_seconds: () => nextSourceId++,
    source_remove: () => true,
};
