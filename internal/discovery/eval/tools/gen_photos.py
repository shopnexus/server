"""Generates the eval photos with gpt-image-2.5 through an OpenAI-compatible
images API: photos/<key>.jpg for each listing from its photo_prompt, and for
each photo query its file, a second shot of the listing it names made with
/images/edits. A file that exists is kept. Run from the server repo:
    IMAGES_API_URL=https://api.openai.com/v1 IMAGES_API_KEY=... \
    python3 internal/discovery/eval/tools/gen_photos.py
"""
import base64
import io
import json
import os
import pathlib
import urllib.request
import uuid

from PIL import Image

DATA = pathlib.Path(__file__).resolve().parent.parent / "testdata"
API = os.environ["IMAGES_API_URL"].rstrip("/") + "/images"
KEY = os.environ["IMAGES_API_KEY"]
STYLE = ", a phone photo for a second-hand listing, the whole item in view, plain background, no text, no people"
AGAIN = "the same item photographed again by its owner, from another angle, in other light, on another background"


def save(b64, path):
    img = Image.open(io.BytesIO(base64.b64decode(b64))).convert("RGB")
    img.thumbnail((768, 768))
    img.save(path, "JPEG", quality=85)


def post(path, body, content_type):
    req = urllib.request.Request(API + path, body, {"Authorization": "Bearer " + KEY, "Content-Type": content_type})
    with urllib.request.urlopen(req, timeout=600) as res:
        return json.load(res)["data"][0]["b64_json"]


def generate(prompt):
    body = json.dumps({"model": "gpt-image-2.5", "prompt": prompt + STYLE, "size": "1024x1024", "n": 1}).encode()
    return post("/generations", body, "application/json")


def edit(photo, prompt):
    boundary = uuid.uuid4().hex
    parts = []
    for name, value in (("model", "gpt-image-2.5"), ("prompt", prompt), ("size", "1024x1024"), ("n", "1")):
        parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'.encode())
    parts.append(
        f'--{boundary}\r\nContent-Disposition: form-data; name="image[]"; filename="{photo.name}"\r\n'
        "Content-Type: image/jpeg\r\n\r\n".encode() + photo.read_bytes() + b"\r\n"
    )
    parts.append(f"--{boundary}--\r\n".encode())
    return post("/edits", b"".join(parts), "multipart/form-data; boundary=" + boundary)


def main():
    photos = DATA / "photos"
    photos.mkdir(exist_ok=True)
    for listing in json.loads((DATA / "listings.json").read_text()):
        out = photos / (listing["key"] + ".jpg")
        if not out.exists():
            save(generate(listing["photo_prompt"]), out)
            print("made", out.name)
    for query in json.loads((DATA / "queries.json").read_text()):
        if query["kind"] != "photo":
            continue
        out = photos / query["photo"]
        if not out.exists():
            save(edit(photos / (query["of"] + ".jpg"), AGAIN), out)
            print("made", out.name)


if __name__ == "__main__":
    main()
