# Changelog

## 1.0.0 (2026-09-13)


### Features

* add dashboard and automatic note sharing ([#8](https://github.com/comarch/git-byline/issues/8)) ([3afc044](https://github.com/comarch/git-byline/commit/3afc044d01a809968b8cbbb1a31cd58257052585))
* add dashboard range mode ([079ee16](https://github.com/comarch/git-byline/commit/079ee162ac8f49bde3f13ce0c877e2e5faf624f3))
* add Git AI and Agent Trace interop ([2872e54](https://github.com/comarch/git-byline/commit/2872e544a176af5a8ed5ddaa94e0f39cdeea46f3))
* add shell event attribution ([82b0bff](https://github.com/comarch/git-byline/commit/82b0bffd16cf6eaa7f9974a24e14391f1a01c442))
* **app:** add stats verify check commands ([2038a98](https://github.com/comarch/git-byline/commit/2038a984635e9e82aaeb7654a477cc9de05927d1))
* **ci:** add forge merge attribution workflows ([5dad701](https://github.com/comarch/git-byline/commit/5dad7010452684c12d5b532b1f18bd2005781e0f))
* **disclosure:** add AI disclosure exports ([53f1873](https://github.com/comarch/git-byline/commit/53f187375a374204ed6921da5aa4a09d0099df26))
* **hooks:** track history rewrite transitions ([a2d8ae7](https://github.com/comarch/git-byline/commit/a2d8ae7d8431ec9fd53d29796531596eb9cadc90))
* implement S3 attribution states ([42db913](https://github.com/comarch/git-byline/commit/42db913949df15f2d89aeb554d07bbd426933e4a))
* initialize git-byline ([af580f9](https://github.com/comarch/git-byline/commit/af580f9cfcfead591d2c0948fd1d052800498f6d))
* **install:** detect agents and install their hooks at user level ([#10](https://github.com/comarch/git-byline/issues/10)) ([620e9b9](https://github.com/comarch/git-byline/commit/620e9b9e3eb26824d130df4e941c8a2dd17a47af))
* merge S5 forge reconstruction slice ([8ca76df](https://github.com/comarch/git-byline/commit/8ca76df5af592dac9ce57760eac12a582ee50b79))
* merge S6 shell attribution slice ([cebadb2](https://github.com/comarch/git-byline/commit/cebadb2bfa8056229a4daf32ce194a4f676514a6))
* merge S7 interop slice ([71a6018](https://github.com/comarch/git-byline/commit/71a6018ff39b5c0526f4396d42cb2c08a5b98bcc))
* record human identity and package git-byline for every agent ([#9](https://github.com/comarch/git-byline/issues/9)) ([5a4b83b](https://github.com/comarch/git-byline/commit/5a4b83b19e73596dd8dcce9af012b0214ebf5c49))
* **report:** add S1 foundation ([28e9f62](https://github.com/comarch/git-byline/commit/28e9f621041dfd4913619522a9bfe78c9f1674a2))
* **rewrite:** preserve attribution through rewrites ([bcacb65](https://github.com/comarch/git-byline/commit/bcacb65213a774b8453019df02be7101412681c3))
* **update:** add local update command and installer update path ([#13](https://github.com/comarch/git-byline/issues/13)) ([c185bf9](https://github.com/comarch/git-byline/commit/c185bf90582478db00d43e81012b9be3c2cbd3cc))


### Bug Fixes

* address S2 review findings ([14c887f](https://github.com/comarch/git-byline/commit/14c887fd0711383ae401722c070183df966c8407))
* address S3 attribution review findings ([37e9aa1](https://github.com/comarch/git-byline/commit/37e9aa1d93de4b67290f5ddf767838c05ea993fd))
* **ci:** harden forge reconstruction ([16de7d5](https://github.com/comarch/git-byline/commit/16de7d5ace0ae264929235e3e92215c079d02898))
* **disclosure:** harden output and totals validation ([ef1ec86](https://github.com/comarch/git-byline/commit/ef1ec864f58b90508bf61232df3885a1ed212692))
* harden history rewrite attribution ([b1c058e](https://github.com/comarch/git-byline/commit/b1c058e9b320eaa80fd0bda43b9f484497ad0a04))
* harden managed hook installation ([90b7bb8](https://github.com/comarch/git-byline/commit/90b7bb8eb14cb48e0beafc46b46287b166c7c540))
* harden S1 parsing, refs, and report totals ([b4e6626](https://github.com/comarch/git-byline/commit/b4e662611f6ee0a02fd5c6a0ef8205aa166f2660))
* harden S6 shell attribution ([345b0b5](https://github.com/comarch/git-byline/commit/345b0b537cbc5e3b50cc4bbb9bbb5dd23d8ef286))
* ignore AppleDouble entries in archive allowlist ([059a392](https://github.com/comarch/git-byline/commit/059a392cf21b3a7581f2b8cc6ca1e7297d7fe890))
* **interop:** address S7 review findings ([69e05b1](https://github.com/comarch/git-byline/commit/69e05b15204807acf10981a522a1bcbd8414ff07))
* preserve attribution state across commits and rebases ([#12](https://github.com/comarch/git-byline/issues/12)) ([19ac087](https://github.com/comarch/git-byline/commit/19ac08755a2670bd2f9707807f519cf42412b0ed))
* **release:** sync claude and gemini plugin versions on release ([#14](https://github.com/comarch/git-byline/issues/14)) ([dfa4de4](https://github.com/comarch/git-byline/commit/dfa4de43969bb3b7c418a07196cf80a43e8cc666))
* sort dashboard trend by commit instant ([bd4bd9f](https://github.com/comarch/git-byline/commit/bd4bd9fd243b33322f850cf38947d5a1348f5b1f))
* suggest git config identity when note write fails ([bb0c454](https://github.com/comarch/git-byline/commit/bb0c454e5d816377d169ff72ee541ff73a26e307))
* **validate:** skip worktree .git pointer in repo scan ([c508a5c](https://github.com/comarch/git-byline/commit/c508a5c86f1567e49de0f067df279fd6138fa8a3))

## Changelog

Release Please maintains this file from Conventional Commits. Each release
lists user-visible changes under its version and date.
