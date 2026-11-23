# Changelog / 更新履歴

All notable changes to kaiHEN Installer, newest first.
Each entry is the GitHub release tag.
kaiHEN Installer の主な変更履歴です (新しい順)。
見出しは GitHub リリースのタグです。

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
