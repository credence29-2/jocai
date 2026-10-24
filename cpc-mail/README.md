# CPC Mail

One window for all your Gmail and Outlook accounts. Send from any of your aliases, each with its own signature. One portable `CPCMail.exe`, no installer, nothing phoning home.

## What's in it

| Feature | What it does |
| --- | --- |
| Unified inbox | Mail from every account in one list, newest first. A color bar shows which account each message is in. Click an account in the sidebar to see just that one. |
| Folders | Inbox, Starred, Sent, Archive, Spam and Trash for every account. Archive, delete and spam all have Undo. |
| Search | Searches all mail in every account at once, using each provider's own search. |
| Aliases | Every address you can send as, grouped by account. Gmail send-as addresses and their signatures are pulled in automatically. Outlook aliases are added by hand, or found automatically on work accounts. |
| Signatures | Each alias has its own signature, display name and reply-to address, and you choose whether the signature goes on replies. Switching the From box swaps the signature in the message. |
| Smart From | Replying to mail sent to sales@ goes out from sales@, with that signature, through the account it belongs to. New messages use the default you pick. |
| Privacy | Remote images are blocked until you click Show images, so tracking pixels can't report when you opened something. Scripts in email never run. Links open in your normal browser. |
| Attachments | Attach by button or drag and drop. Forwards carry the original's attachments. Click an attachment to save it. |

Keyboard: C new, R reply, A reply all, F forward, E archive, Del delete, S star, U unread, J/K next and previous, / search, Ctrl+Enter send.

## One-time setup

Google and Microsoft only let an app read mail once it's registered with them. Because CPC Mail is yours and not a public app, you register it under your own accounts. It's free and takes about 10 minutes each. The steps are also in the app under **Settings > Sign-in setup**.

### Google

1. At [console.cloud.google.com](https://console.cloud.google.com/projectcreate), create a project called CPC Mail.
2. In **APIs and Services > Library**, turn on the **Gmail API**.
3. In **Google Auth Platform**, set up the consent screen as **External**. Under **Data access**, add the scope `https://www.googleapis.com/auth/gmail.modify`.
4. Under **Audience**, click **Publish app**. Apps left in Testing get signed out every 7 days. Google will show "Google hasn't verified this app" when you sign in. Click **Advanced**, then **Go to CPC Mail**. That's expected for a personal app, which can have up to 100 users.
5. Under **Clients**, create an OAuth client of type **Desktop app**. Paste its Client ID and Client secret into CPC Mail.

### Microsoft

1. At [entra.microsoft.com](https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade), go to **App registrations > New registration**. A free personal Microsoft account can do this.
2. Supported account types: **Any Entra ID tenant and personal Microsoft accounts**.
3. Redirect URI: **Public client/native (mobile and desktop)**, `http://localhost`.
4. Under **API permissions**, add the delegated permissions `Mail.ReadWrite`, `Mail.Send`, `User.Read`, `offline_access` and `SMTP.Send`.
5. Paste the **Application (client) ID** into CPC Mail. No secret is needed.

Then go to **Settings > Accounts** and add each account. Your browser opens for sign-in.

## Sending from aliases

- **Gmail:** the alias has to be set up and verified in Gmail first, under **Settings > Accounts > Send mail as**. Click **Sync from Gmail** in CPC Mail to pull it in with its signature. If an address isn't verified, Gmail replaces it with your main address.
- **Outlook.com and Hotmail:** add the alias to your Microsoft account first (account.microsoft.com > Your info > Edit account info > Add email), then add it in CPC Mail under **From addresses and signatures > Add alias**.
- **Microsoft 365 work accounts:** your admin has to allow SMTP AUTH for your mailbox, and allow sending from aliases (`Set-OrganizationConfig -SendFromAliasEnabled $true`).

Why SMTP for Outlook: Microsoft's mail API always sends from the account's main address, even when the message names an alias. So CPC Mail reads Outlook mail through Microsoft Graph and sends it over Outlook's SMTP server with the same sign-in, which keeps the alias in the From line. If SMTP is turned off for a work account, mail from the main address still goes out through Graph, and CPC Mail tells you which aliases can't send.

## Where things are kept

Everything stays on your PC under `%APPDATA%\CPCMail`:

- `config.json` holds accounts, aliases, signatures and settings.
- `tokens.bin` holds the sign-ins, encrypted with Windows DPAPI so only your Windows account on this PC can read them.

No mail is stored. CPC Mail reads it live from Google and Microsoft, which also means it needs an internet connection. Removing an account in CPC Mail doesn't touch the mail on the server. To fully revoke access, also remove CPC Mail at [myaccount.google.com/permissions](https://myaccount.google.com/permissions) or [account.live.com/consent/Manage](https://account.live.com/consent/Manage).

## Running it

Run `CPCMail.exe`. It needs the Microsoft Edge WebView2 Runtime, which comes with Windows 11 and updated Windows 10. If it's missing, CPC Mail opens Microsoft's download page.

Windows shows a SmartScreen warning the first time because the exe isn't code-signed. Click **More info**, then **Run anyway**.

To try it without signing in to anything, run `CPCMail.exe -demo`. It opens with two sample accounts and three aliases. Nothing is sent and nothing is saved.

## How it works

The window is a WebView2 page served by a small web server inside the exe. The server only listens on 127.0.0.1. Every request has to carry a random key handed to the window at startup and a loopback Host header, so web pages and other programs can't use it. Sign-in uses OAuth with PKCE through your normal browser and a one-time local redirect.

| Folder | What's in it |
| --- | --- |
| `server.go`, `providers.go` | The local API and account sign-in |
| `web/index.html` | The interface, embedded in the exe |
| `internal/gmail` | Gmail API client |
| `internal/outlook` | Microsoft Graph client and SMTP sending |
| `internal/mail` | Message building, reply rules, picking the From address |
| `internal/oauth` | Browser sign-in and token refresh |
| `internal/store` | Settings file and encrypted sign-ins |
| `internal/demo` | The sample accounts behind `-demo` |

## Building

You need Go 1.24 or newer. Go builds the Windows exe from Windows, Mac or Linux.

```
./build.sh        # Mac or Linux
.\build.ps1       # Windows PowerShell
```

Output goes to `dist\CPCMail.exe` and `dist\CPCMail-arm64.exe`.

To work on the interface outside Windows, run `go run . -demo`. It serves the same page to your browser.

The icon, manifest and version info are pre-built into `rsrc_windows_*.syso`. To change them, edit `winres/winres.json` and run:

```
go install github.com/tc-hib/go-winres@latest
go-winres make --in winres/winres.json --arch amd64,arm64
```

## Testing

- `go test ./...` runs the unit tests. The Gmail and Outlook clients are tested against fake Google, Microsoft and SMTP servers, including the STARTTLS and XOAUTH2 sign-in, alias From headers, Bcc handling, and the Outlook fallback rules.
- The **CPC Mail build** GitHub workflow runs the tests on Linux and on Windows (for DPAPI), and produces the exes as a downloadable artifact on every push.

## Not in this version

- Conversation view. Messages are listed one by one, not grouped into threads.
- Offline mode and a local mail cache.
- Desktop notifications. The window title and sidebar show unread counts, refreshed every 2 minutes.
- Drafts that sync to the server. Closing the compose window asks before discarding.
- Other providers like Yahoo, iCloud or plain IMAP.
