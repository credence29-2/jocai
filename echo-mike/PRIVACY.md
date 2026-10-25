# Project Echo Mike privacy policy

Last updated October 25, 2026

Project Echo Mike is a personal desktop email program for Windows. It shows mail from Gmail and Outlook accounts that the person running it chooses to connect.

## What it accesses

When you connect a Gmail account, Project Echo Mike asks Google for the `gmail.modify` permission. It uses that permission only to:

- show your messages, folders and attachments
- mark messages read or unread, star them, archive them, move them to spam or trash, and move them back
- send messages you write, from the addresses you choose
- read your Gmail send-as addresses and their signatures so you can send from them

When you connect a Microsoft account, it uses Microsoft Graph and Outlook SMTP for the same purposes.

## Where your data goes

Your mail goes directly between your PC and Google or Microsoft. Project Echo Mike has no server of its own. No mail, contacts or account details are sent to the developer or to anyone else.

Project Echo Mike does not store copies of your mail. It saves your account list, your aliases and signatures, and your settings on your PC under `%APPDATA%\ProjectEchoMike`. Sign-in tokens are encrypted with Windows so only your Windows account on that PC can read them.

## Google user data

Project Echo Mike's use of information received from Google APIs adheres to the [Google API Services User Data Policy](https://developers.google.com/terms/api-services-user-data-policy), including the Limited Use requirements. Gmail data is used only to provide the features above, inside the app, for the person who connected the account. It is not used for advertising, not sold, not transferred to anyone, and not used to train AI models.

## Removing access

Remove an account in **Settings > Accounts** to delete its saved sign-in from your PC. To revoke access at the source, remove Project Echo Mike at [myaccount.google.com/permissions](https://myaccount.google.com/permissions) or [account.live.com/consent/Manage](https://account.live.com/consent/Manage). Deleting the `%APPDATA%\ProjectEchoMike` folder removes everything the app saved.

## Contact

Questions about this policy: open an issue at [github.com/credence29-2/jocai](https://github.com/credence29-2/jocai/issues).
