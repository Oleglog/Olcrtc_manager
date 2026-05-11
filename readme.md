# !!!ВСЕМ РАЗРАБАМ OLCRTC КЛЕНТОВ И ПАНЕЛЕЙ!!!

## через +- неделю будем смержена ветка с серьезными изменениями ломающими всю совместимость, посмотреть изменения можно здесь (https://github.com/openlibrecommunity/olcrtc/tree/refactor/universal-carrier) , советую пеерчитать доку / кинуть ее в ии и на основе этого обновить панели,  тоесть создать ветку с поддержкой новой версии, проверить что все работает, и ждать как ветка refactor/universal-carrier станет master. получается у вас есть неделя чтобы обновить клиенты



<div align="center">

<img src="https://github.com/openlibrecommunity/material/blob/master/olcrtc.png" width="250" height="250">

![License](https://img.shields.io/badge/license-WTFPL-0D1117?style=flat-square&logo=open-source-initiative&logoColor=green&labelColor=0D1117)
![Golang](https://img.shields.io/badge/-Golang-0D1117?style=flat-square&logo=go&logoColor=00A7D0)

</div>


## About
olcRTC - across the sea

Project that allows users to bypass blocking by parasitizing and tunneling on unblocked and whitelisted services in Russia, use legal webRTC services

## Status

Beta
<br>
See all info in [issues](https://github.com/openlibrecommunity/olcrtc/issues)
<br>
Issues? contact us at [@openlibrecommunity](https://t.me/openlibrecommunity)
<br>
Or wait for the release or at least a release
<br>
Community android client: [alananisimov/olcbox](https://github.com/alananisimov/olcbox)

## Read docs for start 

[For noobs](docs/fast.md)

[Manual](docs/manual.md)

[Setting matrix](docs/settings.md)

[Client URI format](docs/uri.md)

[Client subscription format](docs/sub.md)

[Read before ask](docs/about.md)



## Build

```bash
# install mage first
go install github.com/magefile/mage@latest

# build cli + ui
mage build

# build cli only
mage buildCLI

# build cli with b codec, clones b repo, builds libb.so, compiles with -tags b
mage buildCLIB

# cross-compile for linux / windows / darwin
mage cross

# android aar via gomobile
mage mobile

# container image
mage podman
mage docker

# lint / test / clean
mage lint
mage test
mage clean

```

<div align="center">

---


Telegram: [zarazaex](https://t.me/zarazaexe)
<br>
Email: [zarazaex@tuta.io](mailto:zarazaex@tuta.io)
<br>
Site: [zarazaex.xyz](https://zarazaex.xyz)
<br>
Made for: [olcNG](https://github.com/zarazaex69/olcng)


</div>
