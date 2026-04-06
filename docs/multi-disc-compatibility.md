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

---

## Content Install Path Format

When installing Disc 2 as **Content**:

```
{Drive}:\Content\0000000000000000\{TitleID}\00000002\
```

- `{TitleID}` is the 8-character hex TitleID of **Disc 1** (the main game)
- `00000002` is the standard subfolder code for secondary disc/DLC content
- The file(s) from the Disc 2 ISO are placed directly in this folder

Some Disc 2 ISOs contain their content under a path like:
```
content\0000000000000000\FFED2000\FFFFFFFF\
```
These need to be extracted and placed at `00000002\` with the correct TitleID.

---

## Notes

- TitleIDs can be verified via [XboxUnity](http://xboxunity.net/) or by reading the default.xex
- When in doubt, try **Content** install first; it's the safer choice for multi-disc games where Disc 1 launches the game and Disc 2 is referenced as DLC
- After installing Disc 2 as Content, Aurora/FSD will find it automatically when Disc 1 is launched
- If a game has 3+ discs, Disc 2 and beyond typically all go to the same `00000002` folder
