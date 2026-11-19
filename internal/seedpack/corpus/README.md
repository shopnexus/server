# Seed pack corpus

Hand-written Vietnamese texts the seed pack builder uses for reviews, review
replies, profile bios and account names. Every file is JSON with `version: 1`
and `license: "CC0-1.0"`.

## reviews.json

```json
{"version":1,"license":"CC0-1.0","texts":[
  {"role":"seller","stars":5,"cat":"phones","text":"Máy đẹp, pin 90% đúng mô tả"}]}
```

- `role` is the role of the person reviewed: `seller` when a buyer rates the
  seller, `buyer` when the seller rates the buyer.
- `stars` is 1 to 5.
- `cat` is a category root (`phones`, `laptop`, `fashion`, `vehicles`,
  `furniture`, `home`, `baby`, `sports`, `other`) or `any`.
- `text` is at most 500 characters.

## replies.json

```json
{"version":1,"license":"CC0-1.0","texts":[
  {"context":"negative","role":"seller","text":"Mình xin lỗi vì ship lâu..."}]}
```

- `context` is `positive`, `negative` or `neutral`.
- `role` is the role of the person replying, who is the person reviewed:
  `seller`, `buyer` or `any`.

## bios.json

```json
{"version":1,"license":"CC0-1.0","empty_rate":0.35,"bios":[
  {"persona":"mover","text":"Chuyển nhà nên pass lại"}]}
```

`persona` is one of `student`, `office`, `mom`, `tech`, `collector`, `mover`,
`reseller`, `general`. Bios are at most 200 characters.

## names.json

- `family`: family names with weights `w`, close to real frequency (Nguyễn
  about 38%).
- `given`: given names with `g` (`f`, `m` or `u` for either) and weight `w`.
  Some are two syllables, such as "Minh Thư".
- `middle`: middle names per gender, `f` and `m`, with weights.
- `display_patterns`: weighted templates for the display name. The
  placeholders are `{family}`, `{middle}`, `{given}`, `{nickname}`, and
  `{family_ascii}` / `{given_ascii}` (diacritics removed, "Đ" as "D").
  `{given_lower}` is the given name in lower case.
- `nicknames`: display names such as "Tuấn Đồ Cũ" or "mẹ Bin", with `g`.

## Sampling

Pick by weight wherever a file has `w`; elsewhere pick uniformly.

Reviews:

1. Take the role from the side being reviewed.
2. Draw stars, as weights for 5, 4, 3, 2 and 1 stars. Seller reviews:
   0.62, 0.20, 0.09, 0.04, 0.05. Buyer reviews: 0.70, 0.17, 0.06, 0.03,
   0.04, since most buyers pay and collect.
3. Leave the text empty for about 25% of reviews.
4. Otherwise filter by role and stars. With probability 0.5 pick from texts
   whose `cat` is the listing's category root, when there are any; else pick
   from `cat: "any"`.

Replies:

- Map stars to context: 4 and 5 give `positive`, 3 gives `neutral`, 1 and 2
  give `negative`.
- Reply to about 30% of positive and neutral reviews and about 60% of 1
  and 2 star reviews.
- Pick from texts whose `role` is the replier's role or `any`.

Bios: leave the bio empty with probability `empty_rate`. Otherwise pick a
persona that fits the account's listings (`mom` for baby items, `tech` for
phones and laptops, `mover` for furniture) or `general`, then a text.

Names: about 10% of accounts use a nickname of matching gender. The rest
draw a pattern from `display_patterns` (which also has a `{nickname}`
entry), a gender, then family, middle and given names by weight. A given
name with `g: "u"` fits either gender.

## Avatars

The avatar pool is outside git, in `tmp/seedpack-assets/avatars/` in the
umbrella repo: 400 px JPEG squares and an `index.json`. Each entry has
`file` and `kind` (`person`, `pet`, `scenery`, `cartoon`, `object`), and
person entries add `age_band` (`18-24`, `25-34`, `35-44`, `45-59`, `60+`)
and `gender` (`f`, `m`, or `u` when the face is hidden), so the builder can
match an avatar to a name.

## License

Written for this project by hand; no text is scraped. Released under CC0
1.0.
