# Third-party notices

kaiHEN Installer is a launcher/installer. It does not redistribute the
components below in this repository; at runtime it works with files that are
downloaded from their original releases (with hash verification) or bundled
in the release package. All rights remain with their respective authors.

| Component | Author / source | Role |
|---|---|---|
| kaiHEN | [ouncebrew](https://github.com/ouncebrew) | The homebrew enabler this app installs and starts (pinned build 20261114c) |
| CivSwitch Kit engine | [mza1010](https://gitee.com/mza1010) (CivSwitch Kit v1.1.0) | The PC-side engine that runs the FS race and starts kaiHEN |
| CivSwitch / Remote JS Loader | [Gezine](https://github.com/Gezine/CivSwitch) | The in-game loader the console runs; this repo's releases use the ipfix3 build (see the yyoossk/CivSwitch fork) |
| CivSwitch-JPN | [YotsuhaMeru](https://github.com/YotsuhaMeru) | JIS keyboard / IME support for Japanese consoles |
| CivSwitchFS + civ-autoloader | [SeanHeuc](https://github.com/SeanHeuc) | The autoloader save and its payload; the embedded bootstrap in `autoloader_bootstrap.js` is built from the improved fork (yyoossk/CivSwitchFS: timing learning + freeze fix) |
| SWFence | [byizmysh](https://github.com/byizmysh) | OpenWrt firewall used by the router-isolation page |
| Offline WAN Emulator | TheNxtBigThing8 | Raspberry Pi tool used by the WAN emulator page |
| Homebrew catalog apps | Each app's author (Checkpoint, PKSM, Moonlight, RetroArch, mGBA, Chiaki-ng, Dolphin NX2, and others) | Downloaded from their official releases when installed; see the in-app credits page |

The save-file exploit chain builds on the ns2jb work by kurumuz and others.
See the in-app credits page for the full list.
