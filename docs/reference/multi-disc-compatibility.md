# Multi-Disc Game Compatibility

Adapted from [Iso2God by r4dius](https://github.com/r4dius/Iso2God) with additional notes.

## Install Methods

| Method | Description | When to Use |
|--------|-------------|-------------|
| **GOD** | Convert Disc 2 ISO to Games-on-Demand format, same as Disc 1 | Default for sequel discs / expansion discs that are standalone games |
| **Content** | Copy files to `Content\0000000000000000\{TitleID}\00000002\` | Disc 2 is DLC/bonus content that is loaded by Disc 1 (the main game) |

---

## Games with Known Compatibility Notes

### Content install required (Disc 2 = DLC/bonus content loaded by Disc 1)

| Game | TitleID | Disc 2 Notes |
|------|---------|--------------|
| Alan Wake | 4D5308AB | Disc 2 is bonus content; install as Content to `00000002` |
| Alpha Protocol | 555307DC | Disc 2 is bonus content |
| Bayonetta | 5345082C / 53450833 | Disc 2 is bonus content; install as Content |
| Borderlands: Game of the Year Edition | 545407E7 | GOTY Disc 2 / Add-On Content Disc is DLC/bonus content; install as Content. The Add-On Content Disc XEX carries placeholder TitleID `FFED2000` - server overrides to `545407E7` automatically. |
| Borderlands 2: Game of the Year Edition | 5454087C | GOTY Disc 2 is DLC/bonus content (same Title ID as base game); install as Content |
| Brutal Legend | 4541082F | Disc 2 is bonus content |
| Call of Duty: Black Ops | 41560855 | Disc 2 (multiplayer/zombies); install as Content |
| Call of Duty: Modern Warfare 2 | 41560817 | Disc 2 (spec ops); install as Content |
| Call of Duty: Modern Warfare 3 | 41560882 | Disc 2 (spec ops); install as Content |
| Call of Duty: World at War | 41560812 | Disc 2 (multiplayer); install as Content |
| Dante's Inferno | 4541085F | Disc 2 is bonus content |
| Dead Space | 45410850 | Disc 2 is bonus content |
| Dragon Age: Origins | 45410889 | Disc 2 is bonus content |
| L.A. Noire | 524B4005 | Disc 2 is bonus content; install as Content |
| Mass Effect 2 | 4541082E | Disc 2 is bonus content |
| Mass Effect 3 | 4541097C | Disc 2 is bonus content |
| Max Payne 3 | 5254082A | Disc 2 is multiplayer/bonus |
| Rage | 5553083E | Disc 2 is game continuation; install as Content |
| Red Dead Redemption | 5454082B | Disc 2 (Undead Nightmare); install as Content |
| Resident Evil 5 | 5553081A | Disc 2 is bonus content |
| Star Wars: The Force Unleashed II | 4541091B | Disc 2 is bonus content |
| The Elder Scrolls V: Skyrim | 5454086B | Disc 2 is high-res texture pack; install as Content |
| Tom Clancy's Splinter Cell Blacklist | 5553088F | Disc 2 is bonus content |
| Two Worlds II | 4541089C | Disc 2 is bonus content |

### GOD install recommended (Disc 2 = continuation of the game)

| Game | TitleID | Disc 2 Notes |
|------|---------|--------------|
| Deus Ex: Human Revolution | 0B4607F2 | Disc 2 is game continuation; install as GOD |
| Final Fantasy XIII | 4D5307E6 | Disc 2 is game continuation |
| Final Fantasy XIII-2 | 4D5307F1 | Disc 2 is game continuation |
| Forza Motorsport 3 | 4D53082D | Disc 2 (car/track data); install as GOD |
| Forza Motorsport 4 | 4D53087F | Disc 2 (car/track data); install as GOD |
| GTA IV | 5345200A | Disc 2 is game continuation |
| Halo 3: ODST | 4D530877 | Disc 2 is multiplayer/Halo 3 disc |
| Lost Odyssey | 4D530830 | 4-disc game; all discs are GOD |
| L.A. Noire (all discs) | 524B4005 | 3 discs; Disc 1 as GOD, Disc 2/3 as Content |
| The Last Remnant | 5345082D | 2-disc game; both as GOD |
| Too Human | 4D530810 | 2-disc game |
| Grand Theft Auto V | 545408A7 | Disc 2 is the bootable "Play" disc; install as GOD. **Reversed from every other row here** - see the callout below for Disc 1. |

---

## Special case: GTA V's executable-less Install disc

GTA V ships on two discs, but unlike every other multi-disc title above, **Disc 1 is the odd one out, not Disc 2**:

- **Disc 1 ("Install" disc)**: pure data, **no `default.xex`/`default.xbe` at all** - the Xbox dashboard auto-installs its `Content/` package without ever booting an executable. GOD is structurally impossible (there's no XEX to pull container metadata from), so this must always be installed as **Content**, regardless of the compat table above.
- **Disc 2 ("Play" disc)**: the normal bootable disc; install as **GOD**, per the table row above.

Because Disc 1 has no executable, `ProbeISODiscInfo` can't read a TitleID from it the way it does for every other disc. The server resolves it two ways instead, tried in order:
1. `ProbeContentPackageTitleID` reads the real TitleID straight from the STFS header of the content package embedded in the ISO's `content/0000000000000000/` folder.
2. If that fails, `GuessTitleIDFromMultiDiscName`/`IsNoExecutableInstallDiscName` recognize the release name ("Grand Theft Auto V" / "GTA V" + "Disc 1") and use the known TitleID `545408A7` directly.

See GODSend-360 issue #4.

---

## Content Install Path Format

When installing Disc 2 as **Content**:

```
{Drive}:\Content\0000000000000000\{TitleID}\00000002\
```

- `{TitleID}` is the 8-character hex TitleID of **Disc 1** (the main game)
- `00000002` is the standard subfolder code for secondary disc/DLC content
- The file(s) from the Disc 2 ISO are placed directly in this folder

Some Add-On Content Discs (used by many publishers, e.g. 2K Games for Borderlands GOTY) carry a generic placeholder Title ID `FFED2000` in their `default.xex`, with content stored under:
```
content\0000000000000000\FFED2000\00000002\
```
The server automatically resolves the correct parent Title ID by reading the STFS header (offset `0x0360`) of the first content package in that directory. The resolved Title ID is used as the install destination so Aurora/FSD correctly associates the DLC with the parent game. A game-name heuristic is used as a fallback if the package probe fails.

---

## Notes

- TitleIDs can be verified via [XboxUnity](http://xboxunity.net/), [XboxDB](https://xboxdb.altervista.org/), or by reading the default.xex
- When in doubt, try **Content** install first; it's the safer choice for multi-disc games where Disc 1 launches the game and Disc 2 is referenced as DLC
- After installing Disc 2 as Content, Aurora/FSD will find it automatically when Disc 1 is launched
- If a game has 3+ discs, Disc 2 and beyond typically all go to the same `00000002` folder
