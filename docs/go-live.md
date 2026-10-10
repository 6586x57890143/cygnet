# Go live

After the first merge to `main` has built the image:

```sh
go run ./tools/setup
```

It asks for the bot token (hidden), checks it with Discord, makes the application's
owner the bootstrap admin, writes both to the host, sets the bot's avatar and banner,
reports the privileged intents, deploys through GitHub Actions, waits for the bot to
connect, and prints the invite. Safe to run again at any time, for example to rotate
the token.

Then add cygnet to a server from the install link setup prints. cygnet is a private
app: only its owner can use the link. What it asks for is written by cygnet
itself at every boot: the bot and its slash commands, plus the permissions the running
modules declare. A module's new permission reaches the link on the deploy that ships it.
