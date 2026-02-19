import GObject from 'gi://GObject';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as QuickSettings from 'resource:///org/gnome/shell/ui/quickSettings.js';

import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import {IntuneToggle} from './quickToggle.js';
import {ContainerManager} from './containerManager.js';

const IntuneIndicator = GObject.registerClass(
class IntuneIndicator extends QuickSettings.SystemIndicator {
    _init(extensionObject) {
        super._init();

        this._manager = new ContainerManager();
        this._toggle = new IntuneToggle(this._manager);
        this.quickSettingsItems.push(this._toggle);
    }

    destroy() {
        this._manager.destroy();
        this.quickSettingsItems.forEach(item => item.destroy());
        super.destroy();
    }
});

export default class IntuneExtension extends Extension {
    enable() {
        this._indicator = new IntuneIndicator(this);
        Main.panel.statusArea.quickSettings.addExternalIndicator(this._indicator);
    }

    disable() {
        this._indicator.destroy();
        this._indicator = null;
    }
}
