# Changelog / 更新履歴

All notable changes to kaiHEN Installer, newest first.
Each entry is the GitHub release tag.
kaiHEN Installer の主な変更履歴です (新しい順)。
見出しは GitHub リリースのタグです。

## 20261128-0006

- The update history page now also shows the update history of the catalog software and apps: pick kaiHEN itself or any catalog app to see its release history (versions, dates, and notes) from the publisher.
- 更新履歴のページで、カタログのソフトとアプリの更新履歴も見られるようにしました。kaiHEN 本体やカタログのアプリを選ぶと、公開元のリリース履歴 (バージョン・日付・説明) が表示されます。

## 20261127-2349

- An update history page was added to the app: the full changelog can be checked in the app, newest first, in the display language (English or Japanese).
- アプリに更新履歴のページを追加しました。全ての更新履歴を、表示中の言語 (英語または日本語) で新しい順にアプリ内で確認できます。

## 20261127-2207

- The stock kaiHEN is now 20261127.1, the newest release, and the patched type keeps its own base pin (kaiHEN 20261117.1) instead of following it.
- To update kaiHEN, just update this app and start kaiHEN with the stock type: the new release is downloaded and prepared automatically.
- When you start 20261127.1 for the first time, restart the console first if an older kaiHEN has run since the last restart (an upstream requirement for this release).
- 20261127.1 brings back support for dynamically linked homebrew, and the shared libraries it needs (kaihen/lib) are now copied to the SD card automatically when you install homebrew from the catalog.
- 純正版の kaiHEN を最新の 20261127.1 にし、パッチ版の土台は追従させず kaiHEN 20261117.1 に別で固定しました。
- kaiHEN をアップデートするには、このアプリを更新して純正版のまま kaiHEN を起動するだけです。新しいリリースは自動でダウンロードして用意します。
- 20261127.1 を初めて起動するときは、前回の再起動後に古い kaiHEN を起動したことがある場合、先に本体を再起動してください (この版の上流の要件です)。
- 20261127.1 で動的リンクの自作ソフトへの対応が戻り、必要な共有ライブラリ (kaihen/lib) を、カタログから自作ソフトをインストールするときに SD カードへ自動でコピーするようにしました。

## 20261127-2153

- N Launcher (by davidkalil10) was added to the stock kaiHEN catalog page, for 38 catalog apps in total.
- N Launcher prepares and launches NUP game backups directly on the console, with no PC conversion step.
- It runs on stock kaiHEN and keeps its saves separate from the console's own saves.
- N Launcher (davidkalil10 作) を純正 kaiHEN 用のカタログページに追加し、カタログは全38本になりました。
- N Launcher は、NUP 形式のゲームバックアップを PC での変換なしに本体だけで準備・起動するローダーです。
- 純正 kaiHEN で動き、セーブは本体のセーブとは別に保持されます。

## 20261127-1744

- On Android, the app now asks for the install permission its self-update needs the first time it starts, instead of when an update is found.
- The prompt explains why the permission is needed and opens the system settings; it appears only once.
- If the permission is skipped, the update flow still opens the same settings screen when an update actually runs.
- No other changes.
- Android 版で、自動更新に必要なインストール許可を、更新が見つかったときではなく初回起動時に求めるようにしました。
- 許可がなぜ必要かを説明してからシステムの設定を開く案内を、初回に1回だけ出します。
- 許可しなかった場合も、実際に更新するときに従来どおり同じ設定画面を開きます。
- 他の変更はありません。

## 20261127-1715

- The kaiHEN page now lets you choose the kaiHEN type: stock or patched.
- The patched type is the stock kaiHEN 20261117.1 with switch2-modbridge's ro:dmnt binaries overlaid, each file verified against a pinned hash.
- The kaiHEN pin moved from 20261123 to 20261117.1, because upstream deleted the 20261123 release and 20261117.1 is the newest release that still exists.
- Switching types re-prepares kaiHEN the next time it starts; starting through the autoloader is unchanged.
- The homebrew catalog is split into two pages: 36 apps for stock kaiHEN, and a new page for patched kaiHEN.
- KaiVesper (by Sank-kab) was added to the patched page, for 37 catalog apps in total.
- KaiVesper loads unencrypted ExeFS/RomFS game dumps through a host title on the console, with hot-swapping between games and per-game save isolation; it needs the patched kaiHEN.
- 「kaiHENを導入」のページで、kaiHEN の種類 (純正版 / パッチ版) を選べるようにしました。
- パッチ版は、純正の kaiHEN 20261117.1 に switch2-modbridge の ro:dmnt バイナリを重ねたもので、各ファイルは固定したハッシュで検証します。
- kaiHEN のピンを 20261123 から 20261117.1 に移しました。20261123 は上流で削除され、現存する最新は 20261117.1 のためです。
- 種類を変えると、次に kaiHEN を起動するときに用意し直します。オートローダーで起動する場合は従来どおりです。
- 自作ソフトのカタログを2ページに分けました。純正 kaiHEN 用の36本と、パッチ版 kaiHEN 用の新しいページです。
- パッチ版のページに KaiVesper (Sank-kab 作) を追加し、カタログは全37本になりました。
- KaiVesper は、暗号化されていない ExeFS/RomFS のゲームダンプを本体上のホストタイトル経由で起動するソフトで、ゲームのホットスワップとゲームごとのセーブ分離に対応します。パッチ版 kaiHEN が必要です。

## 20261126-1427

- One app was added to the homebrew catalog, for 36 apps in total.
- Local Update Tool (by AlexTech01) lets a kaiHEN console install game updates sent from another console through "Match Version with Local Users" that would normally be rejected.
- The catalog installs it as /switch/local-update-tool/local-update-tool.nro, the layout its release asks for.
- The catalog picks up the newest upstream releases automatically at install time; SW2PS2 has now graduated to the stable v1.2.0.
- The kaiHEN pin (20261123) and the autoloader bootstrap pin (stock-20261125b) are unchanged.
- 自作ソフトのカタログに1本を追加し、全36本になりました。
- Local Update Tool (AlexTech01 作) は、他の本体から「近くのユーザーとバージョンを合わせる」で送られてきたゲーム更新のうち、通常は拒否されるものを kaiHEN 搭載の本体へインストールできるツールです。
- カタログからは、リリースの指定どおり /switch/local-update-tool/local-update-tool.nro にインストールします。
- カタログはインストール時に上流の最新版を自動で取得します。SW2PS2 は安定版 v1.2.0 になりました。
- kaiHEN のピン (20261123) とオートローダーのブートストラップのピン (stock-20261125b) は変更ありません。

## 20261125-1146

- Updated the pinned kaiHEN to 20261123, the first release with dynamically linked homebrew.
- The distribution layout changed (the SD tree now sits under sdcard/), so the installer places the files its launch engine expects and copies the shared libraries (kaihen/lib) to the SD card over FTP during catalog installs.
- Statically linked homebrew runs as before.
- Upstream still marks 20261123 as a work in progress, and launching it through this installer is not yet verified on real hardware; if it does not start, reinstall the previous build (20261125-0259), which downloads kaiHEN 20261117.1 again automatically.
- SW2PS2 now tracks the newest release overall, including the experimental prereleases (currently v1.1.0-experimental.2), instead of only the newest stable.
- KaiPlayer v1.6.0, KaiRadio v1.6.0, KaiVideo v1.17.0 and PKForge v0.7.5 are the newest upstream releases; the catalog picks them up automatically at install time.
- kaiHEN のピンを、動的リンクの自作ソフトに対応した初のリリース 20261123 に更新しました。
- 配布レイアウトが変わった (SD ツリーが sdcard/ 配下になった) ため、インストーラーが起動エンジン向けのファイルを配置し、共有ライブラリ (kaihen/lib) はカタログのインストール時に FTP で SD カードへコピーします。
- 静的リンクの自作ソフトは従来通り動きます。
- 上流では 20261123 はまだ WIP 扱いで、このインストーラー経由での起動は実機未検証です。起動しない場合は前のビルド (20261125-0259) を入れ直してください。kaiHEN 20261117.1 を自動で取り直します。
- SW2PS2 は安定版だけでなく実験版プレリリースも含めた最新版 (現在 v1.1.0-experimental.2) を取るようになりました。
- KaiPlayer v1.6.0、KaiRadio v1.6.0、KaiVideo v1.17.0、PKForge v0.7.5 が上流の最新版です。カタログはインストール時に自動で取得します。

## 20261125-0259

- Fixed a bug that made zip-format catalog apps fail to install after an app restart, ending with "Cannot read the payload file".
- The startup refresh of catalog definitions overwrote the resolved format (zip) with the seed value, so a downloaded SD zip was mistaken for a bare .nro file.
- Every zip-format app was affected, including WinampNX2, RetroArch, the FTPD Plugin, melonDS-switch2 and Reset Launch Version.
- The catalog is unchanged at 35 apps, and the autoloader bootstrap pin is unchanged (stock-20261125b).
- zip 形式のカタログソフトが、アプリの再起動後にインストールできず「ペイロードファイルが読めません」と失敗する不具合を修正しました。
- 起動時のカタログ定義の更新が、解決済みの形式 (zip) を初期値で上書きしてしまい、ダウンロードした SD zip を .nro ファイルと誤認していました。
- WinampNX2、RetroArch、FTPD プラグイン、melonDS-switch2、Reset Launch Version など、zip 形式の全ソフトが対象でした。
- カタログは全35本のまま変更ありません。オートローダーのブートストラップのピンも変更ありません (stock-20261125b)。

## 20261125-0244

- Three apps were added to the homebrew catalog, for 35 apps in total.
- melonDS-switch2 (by ac1dburnz) is an experimental Nintendo DS emulator port, a prerelease build that runs on the interpreter, so performance is limited.
- PKForge (by sh00kapic) is an offline Pokémon save editor; the catalog installs the AIO edition as /switch/PKForge/PKForge.nro, the layout its release asks for.
- Reset Launch Version (by cainan) resets a title's launch version to 1.0.0, so the lowest version on the cartridge can run again after a newer version was used.
- It does not install or remove updates, and it does not change save data.
- The autoloader bootstrap pin is unchanged (stock-20261125b).
- 自作ソフトのカタログに3本を追加し、全35本になりました。
- melonDS-switch2 (ac1dburnz 作) はニンテンドーDS エミュレータの実験的な移植で、プレリリース版です。インタプリタ動作のため性能には制限があります。
- PKForge (sh00kapic 作) はオフラインで使えるポケモンのセーブエディタです。カタログからは AIO 版を、リリースの指定どおり /switch/PKForge/PKForge.nro にインストールします。
- Reset Launch Version (cainan 作) はタイトルの起動バージョンを 1.0.0 にリセットし、新しいバージョンを使った後でもカードリッジの一番古いバージョンを再び起動できるようにします。
- アップデートのインストールや削除は行わず、セーブデータも変更しません。
- オートローダーのブートストラップのピンは変更ありません (stock-20261125b)。

## 20261125-0147

- Documentation correction: the README now lists only the three fixes that are actually in the current bootstrap (stock-20261125b).
- The earlier improvement list (timing learning and the others) was removed from the README, because those changes are not in the current release.
- The fork READMEs (CivSwitchFS, civ-autoloader) were rewritten the same way.
- Nothing else changes from build 20261125-0111: the bootstrap pin is the same stock-20261125b.
- 説明文の訂正です。README に載せるのは、現在のブートストラップ (stock-20261125b) に実際に入っている3点の修正だけにしました。
- 以前の改良一覧 (タイミング学習など) は、現在のリリースに入っていないため README から外しました。
- フォークの README (CivSwitchFS、civ-autoloader) も同じように書き直しました。
- それ以外は build 20261125-0111 から変更ありません。ブートストラップのピンは同じ stock-20261125b です。

## 20261125-0111

- The autoloader bootstrap is now stock-20261125b as the standard configuration (the same pin as build 20261125-0046).
- On the test console it booted on the first try, where every improved payload stopped at the first step.
- The fork's improvements 1 to 5 (timing learning, the freeze fix, and the retry changes) are not in the current bootstrap.
- Which of them breaks the first step is still under investigation.
- This build only updates the documentation to match the current configuration.
- Nothing else changes from build 20261125-0046.
- オートローダーのブートストラップが、正式な構成として stock-20261125b になりました (ピンは build 20261125-0046 と同じです)。
- テスト機では、改良版ペイロードが全て最初の段階で止まる中、これだけが一発で起動しました。
- フォークの改良1〜5 (タイミング学習、フリーズ修正、リトライ変更) は現在のブートストラップには入っていません。
- そのどれが最初の段階を止めているかは調査中です。
- この build は、現在の構成に合わせて説明文を更新しただけです。
- それ以外は build 20261125-0046 から変更ありません。

## 20261125-0046

- Diagnostic build: the autoloader now installs the stock-20261125b bootstrap.
- That is the stock upstream payload plus only the two changes improved-20261124 added: the OperateRange packet size (0x0E words) and the 7 MiB dirty file.
- Still no timing learning and no freeze fix.
- This is the next step of the diagnosis ladder: after this, the fallbacks are the plain stock payload (build 20261125-0040) and then improved-20261117b (build 20261125-0030).
- Nothing else changes from build 20261125-0040.
- 切り分け用の build です。オートローダーが stock-20261125b のブートストラップを入れるようになりました。
- 純正ペイロードに、improved-20261124 で追加した2点 (OperateRange のパケットサイズ 0x0E 語と、ダーティファイル 7MiB) だけを足したものです。
- タイミング学習とフリーズ修正は入っていません。
- 切り分けの梯子の次の一手です。だめな場合の戻し先は、純正ペイロード (build 20261125-0040)、次に improved-20261117b (build 20261125-0030) です。
- それ以外は build 20261125-0040 から変更ありません。

## 20261125-0040

- Diagnostic build: the autoloader now installs the stock-20261125 bootstrap.
- That is the completely stock upstream CivSwitchFS payload, with none of the fork's improvements (no timing learning, no freeze fix).
- Only its save installer adds the save-folder creation.
- On the test console, the improved-20261117 payload failed at the first boot step in exactly the same way as improved-20261124, so the cause is not the recent payload changes.
- This build checks whether the stock payload behaves the same on that console.
- It is a test build, not a recommended configuration.
- Nothing else changes from build 20261125-0030.
- 切り分け用の build です。オートローダーが stock-20261125 のブートストラップを入れるようになりました。
- 中身は完全に純正の upstream CivSwitchFS ペイロードで、このフォークの改良は一切入っていません (タイミング学習なし、フリーズ修正なし)。
- セーブのインストーラーだけフォルダ作成を追加しています。
- テスト機では improved-20261117 のペイロードも improved-20261124 と全く同じく起動の最初の段階で失敗したため、原因は最近のペイロード変更ではありません。
- この build は、純正ペイロードでも同じ本体で同じ失敗になるかを確認するためのものです。
- テスト用であり、常用を勧める構成ではありません。
- それ以外は build 20261125-0030 から変更ありません。

## 20261125-0030

- The autoloader now installs the improved-20261117b bootstrap.
- Its Load Game payload is byte-identical to improved-20261117 (timing learning + freeze fix).
- Its save installer creates the save:/saves and save:/saves/Single folders first when they are missing, like improved-20261124b.
- Build 20261125-0000 could not install the plain improved-20261117 bootstrap on consoles whose save data has no saves/Single folder ("create : 0x202").
- Nothing else changes from build 20261125-0000.
- オートローダーが improved-20261117b のブートストラップを入れるようになりました。
- 「ゲームをロード」で動くペイロードは improved-20261117 とバイト単位で同一です (タイミング学習+フリーズ修正)。
- セーブのインストーラーは、improved-20261124b と同様に、フォルダが無い場合に save:/saves と save:/saves/Single を先に作ります。
- build 20261125-0000 は、saves/Single フォルダが無い本体では素の improved-20261117 を入れられませんでした (「create : 0x202」)。
- それ以外は build 20261125-0000 から変更ありません。

## 20261125-0000

- This build rolls the autoloader bootstrap back to improved-20261117 (timing learning + freeze fix).
- On a real console, the improved-20261124 payload stopped immediately at the first step of every boot ("Could not open the required file systems."), before the timing race even started.
- The cause is still under investigation.
- The retry improvements, the OperateRange packet fix, the 7 MiB dirty file, and the installer's save-folder creation are not in the bootstrap this build installs.
- Nothing else changes from build 20261124-2314.
- オートローダーのブートストラップを improved-20261117 (タイミング学習+フリーズ修正) に戻しました。
- improved-20261124 のペイロードは、実機で毎回起動直後の最初の段階で止まってしまいました (「Could not open the required file systems.」で、タイミングの抽選が始まる前です)。
- 原因は調査中です。
- リトライ改良・OperateRange のパケット修正・ダーティファイル 7MiB・インストーラーのセーブフォルダ作成は、この build が入れるブートストラップには含まれません。
- それ以外は build 20261124-2314 から変更ありません。

## 20261124-2314

- Fixed the autoloader save install failing with "create : 0x202" when the console's save data has no saves/Single folder.
- The autoloader now installs the improved-20261124b bootstrap, whose installer creates the save folders first when they are missing.
- The payload that runs at Load Game is unchanged from improved-20261124.
- While the autoloader save installs, an "Installing…" dialog is now shown until the result appears.
- To use it, reinstall the autoloader save once from the autoloader page.
- Reinstalling resets the timing learning record.
- 本体のセーブデータに saves/Single フォルダが無い場合に、オートローダーセーブのインストールが「create : 0x202」で失敗する問題を直しました。
- オートローダーが improved-20261124b のブートストラップを入れるようになりました。
- そのインストーラーは、フォルダが不足している場合に先に作成します。
- 「ゲームをロード」で動くペイロード自体は improved-20261124 から変更ありません。
- オートローダーセーブを入れている間、結果が出るまで「インストール中…」のダイアログを表示するようになりました。
- 使うには、オートローダーのページからセーブを1回入れ直してください。
- 入れ直すとタイミング学習の記録は初期化されます。

## 20261124-2144

- The autoloader now installs the improved-20261124 bootstrap.
- Its cache-invalidate commands (OperateRange) now declare the correct packet size, 0x0E words instead of 0x0C.
- The old value caused no observed failure, but the new one matches the reference implementation.
- Its dirty file is now 7 MiB instead of 8 MiB, leaving 1 MiB of headroom under the save-data cap.
- To use it, reinstall the autoloader save once from the autoloader page.
- Reinstalling resets the timing learning record.
- オートローダーが improved-20261124 のブートストラップを入れるようになりました。
- キャッシュ無効化の命令 (OperateRange) のパケットサイズが、正しい 0x0E 語になりました (従来は 0x0C 語)。
- 古い値でも失敗した形跡はありませんが、参照実装と一致するようになりました。
- ダーティファイルが 8MiB から 7MiB になり、セーブデータ上限に対して 1MiB の余裕ができました。
- 使うには、オートローダーのページからセーブを1回入れ直してください。
- 入れ直すとタイミング学習の記録は初期化されます。

## 20261123-2229

- Fixed the explanation at the top of the homebrew install page not breaking lines at sentence ends.
- The text is re-set every time the page opens, and that re-set skipped the line-break processing.
- 自作ソフトをインストールのページ上部の説明文が、文ごとに改行されていなかったのを直しました。
- ページを開くたびに文章を貼り直す作りで、その貼り直しで改行処理が抜けていました。

## 20261123-2221

- Each item in the gallery grid now has its own download button, like in Vita Gallery.
- You no longer need to open the viewer to download a single photo or video.
- Sentence line breaks were re-checked on every page and in all 12 languages.
- The one hint text that was not breaking (on the SWFence page) now breaks too.
- ギャラリーの一覧の1件ずつに、Vita Gallery と同じようにダウンロードボタンを付けました。
- 1枚だけ落とすのに拡大表示を開かなくて済みます。
- 全ページ・全12言語で、文ごとの改行にもれがないか改めて確認しました。
- 改行されていなかった1か所 (SWFence ページのヒント) も直しました。

## 20261123-1949

- The gallery now supports multi-select.
- Check items, then download them together as one zip file (album.zip), or clear the selection.
- There is still no delete in the gallery, on purpose.
- Dialog messages now break lines at sentence ends, like the page notes already did.
- The README was reformatted to one sentence per line, and this changelog was added.
- ギャラリーで複数選択ができるようになりました。
- チェックを入れた分をまとめて ZIP (album.zip) でダウンロードしたり、選択を解除したりできます。
- ギャラリーに削除機能は今まで通りありません。
- ダイアログの文章も、ページの説明文と同じく文ごとに改行するようになりました。
- README を文ごとに改行する書式に整え、この更新履歴を追加しました。

## 20261123-1847

- Large-file FTP uploads (such as Chromium NX's 411 MB image) now retry on the other FTP server when a transfer drops partway.
- The log shows which server was used and the exact failure cause.
- A transfer may now take up to an hour.
- 大きなファイルの FTP 送信 (Chromium NX の 411MB イメージ等) で、途中で切れたらもう一方の FTP サーバーで再試行するようになりました。
- どのサーバーを使ったかと、失敗した正確な理由がログに残ります。
- 転送の制限時間を1時間まで延ばしました。

## 20261123-1838

- Gallery downloads now show a "Downloading…" dialog until the download finishes, then flip to the result.
- ギャラリーのダウンロード中は「ダウンロードしています…」ダイアログが表示され、終わると結果に切り替わるようになりました。

## 20261123-1828

- Fixed Chromium NX failing to install for users who had installed it before.
- The homebrew catalog is now sorted by name.
- 以前にインストールしたことがある場合に Chromium NX のインストールが失敗する問題を直しました。
- 自作ソフトのカタログを名前順に並べるようになりました。

## 20261123-1818

- Added Cemu NX2 to the homebrew catalog (32 apps).
- Catalog installs now show an "Installing…" dialog until they finish.
- 自作ソフトのカタログに Cemu NX2 を追加しました (全32本)。
- カタログのインストール中は「インストール中…」ダイアログが完了まで表示されるようになりました。

## 20261123-1759

- Installing Chromium NX now also downloads its 411 MB image (chromium-full.ext4) from the latest release and puts it into /switch/chromium-nx on the console.
- Chromium NX のインストール時に、411MB のイメージファイル (chromium-full.ext4) も最新リリースから自動でダウンロードして、本体の /switch/chromium-nx に入れるようになりました。

## 20261123-1636

- Catalog installs now fall back to the FTPD plugin (port 5000) when OunceShell's FTP (port 1337) does not answer.
- The gallery view got a Refresh button.
- A Home button was added to the top bar.
- カタログのインストールが、OunceShell の FTP (ポート 1337) でだめな場合に FTPD プラグイン (ポート 5000) へ自動で切り替わるようになりました。
- ギャラリー表示に「更新」ボタンを追加しました。
- 上部に「ホーム」ボタンを追加しました。

## 20261122-2208

- Added WinampNX2, Eden for SW2 (POC), Chromium NX and Vita3K to the homebrew catalog.
- 自作ソフトのカタログに WinampNX2、Eden for SW2 (POC)、Chromium NX、Vita3K を追加しました。

## 20261121-1203

- The autoloader bootstrap was updated to improved-20261121.
- It ports the retry-reliability changes (cache precheck, cache invalidation during cleanup, retune threshold) from the slop line of CivSwitchFS-kaiHEN.
- Reinstall the autoloader save once to apply it.
- オートローダーのブートストラップを improved-20261121 に更新しました。
- CivSwitchFS-kaiHEN の slop 系統から、リトライの信頼性向上 (キャッシュの事前チェック、クリーンアップ時のキャッシュ無効化、再調整のしきい値) を移植しています。
- 適用にはオートローダーセーブの入れ直しが1回必要です。

## 20261121-1125

- Added OunceMTP to the homebrew catalog.
- 自作ソフトのカタログに OunceMTP を追加しました。

## 20261121-1037

- Added KaiDump and SW2PS2 to the homebrew catalog.
- 自作ソフトのカタログに KaiDump と SW2PS2 を追加しました。

## 20261120-2030

- The autoloader page now explains the "Could not open the required file systems / Cleanup failed" stop screen.
- It is a safe stop after file-system acquisition failed, not a freeze.
- Fully power the console off and on, then Load Game again.
- オートローダーのページに「Could not open the required file systems / Cleanup failed」の停止画面の説明を追加しました。
- これはファイルシステムの取得に失敗したときの安全な停止で、フリーズではありません。
- 本体の電源を完全に切って入れ直してから、もう一度ゲームをロードしてください。

## 20261119-0356

- SWFence now installs fully offline.
- Missing dependencies are recorded during the connection check, downloaded while you are back on an internet connection, and installed from files only.
- SWFence のインストールを完全オフライン化しました。
- 不足している依存パッケージは接続確認のときに記録され、インターネットにつながる場所でダウンロードしておき、インストールはファイルだけで行います。

## 20261118-2227

- The gallery is now view and download only.
- Deleting an album file over FTP leaves an entry in the console's album that cannot be deleted even on the console (error 2206-0023), so the delete feature was removed.
- ギャラリーを表示とダウンロードだけにしました。
- FTP でアルバムのファイルを消すと本体のアルバムに項目だけが残り、その項目は本体側でも消せなくなる (エラー 2206-0023) ため、削除機能を撤去しました。

## 20261118-2038

- Fixed the Android self-update never opening the install screen.
- Android の自己更新でインストール画面が開かない問題を直しました。

## 20261118-1925

- kaiHEN is pinned to 20261117.1.
- All bundled binaries are used exactly as published, including kaiHEN's official fix for the textInit 2345-0011 error.
- kaiHEN のピンを 20261117.1 にしました。
- 同梱バイナリはすべて公開ファイルそのまま使います。
- textInit 2345-0011 エラーには kaiHEN 公式の修正が含まれます。

## 20261117-2112

- The Android app was released.
- It has the same screens and features as the Windows version, plus a dark theme.
- Self-update was added: the app checks for a newer release at startup and updates itself.
- Android 版を公開しました。
- 画面と機能は Windows 版と同じで、ダークテーマもあります。
- 自己更新を追加しました。
- 起動時に新しい版を確認して、そのまま更新できます。

## 20261117-0327

- First public release.
- 初回公開。
