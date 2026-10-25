# JenkTui VHS demo

The reproducible VHS source is [`jenktui-demo.tape`](jenktui-demo.tape). Its
rendered documentation asset is the optimized animated
[`README GIF`](../assets/jenktui-demo.gif).

## Render

Install [VHS](https://github.com/charmbracelet/vhs) and DroidSansM Nerd Font,
then prepare the repository's local Jenkins data:

```bash
task jenkins:lts
task seed
task demo
```

`task demo` builds JenkTui, records a temporary MP4 in `dist/`, uses FFmpeg to
create the animated GIF embedded in the README, and then deletes the MP4.

> **Note:** The scripted tour triggers one `mobile-app` build and replays one
> completed run on the configured Jenkins controller. Use the disposable local
> controller rather than a production server.
