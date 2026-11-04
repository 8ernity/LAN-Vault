#!/usr/bin/env python3
import os
import shutil
from PIL import Image, ImageOps

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC_IMAGE = r"D:\Downloads\LANVault.png"

def make_square(img, bg_color=(14, 17, 22, 255)):
    # Create square canvas fitting maximum dimension
    max_side = max(img.width, img.height)
    square = Image.new("RGBA", (max_side, max_side), bg_color)
    x = (max_side - img.width) // 2
    y = (max_side - img.height) // 2
    square.paste(img, (x, y), img if img.mode == 'RGBA' else None)
    return square

def generate_icons():
    if not os.path.exists(SRC_IMAGE):
        raise FileNotFoundError(f"Source image not found: {SRC_IMAGE}")

    orig = Image.open(SRC_IMAGE).convert("RGBA")
    print(f"Loaded source image: {SRC_IMAGE} ({orig.size})")

    # Corner pixel color for natural background blending
    corner_color = orig.getpixel((0, 0))
    print(f"Corner color: {corner_color}")

    # Use corner color or theme dark background #0E1116
    square = make_square(orig, bg_color=corner_color)

    # 1. Desktop Build Assets
    build_dir = os.path.join(ROOT, "desktop", "build")
    win_dir = os.path.join(build_dir, "windows")
    os.makedirs(win_dir, exist_ok=True)

    appicon_path = os.path.join(build_dir, "appicon.png")
    square.resize((512, 512), Image.Resampling.LANCZOS).save(appicon_path, "PNG")
    print(f"Saved: {appicon_path}")

    # Multi-resolution Windows ICO
    ico_path = os.path.join(win_dir, "icon.ico")
    square.save(
        ico_path,
        format="ICO",
        sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
    )
    print(f"Saved: {ico_path}")

    # 2. Desktop Frontend Assets
    frontend_img_dir = os.path.join(ROOT, "desktop", "frontend", "src", "assets", "images")
    os.makedirs(frontend_img_dir, exist_ok=True)
    logo_universal = os.path.join(frontend_img_dir, "logo-universal.png")
    square.resize((512, 512), Image.Resampling.LANCZOS).save(logo_universal, "PNG")
    print(f"Saved: {logo_universal}")

    frontend_public_dir = os.path.join(ROOT, "desktop", "frontend", "public")
    os.makedirs(frontend_public_dir, exist_ok=True)
    public_logo = os.path.join(frontend_public_dir, "logo.png")
    square.resize((512, 512), Image.Resampling.LANCZOS).save(public_logo, "PNG")
    print(f"Saved: {public_logo}")

    # 3. Android Mipmap Densities
    android_res = os.path.join(ROOT, "android", "app", "src", "main", "res")
    densities = {
        "mipmap-mdpi": 48,
        "mipmap-hdpi": 72,
        "mipmap-xhdpi": 96,
        "mipmap-xxhdpi": 144,
        "mipmap-xxxhdpi": 192,
    }

    for dir_name, size in densities.items():
        density_dir = os.path.join(android_res, dir_name)
        os.makedirs(density_dir, exist_ok=True)
        
        # Remove legacy webp files if present
        for f in ["ic_launcher.webp", "ic_launcher_round.webp"]:
            p = os.path.join(density_dir, f)
            if os.path.exists(p):
                os.remove(p)
                print(f"Removed legacy: {p}")

        # Square icon
        launcher_png = os.path.join(density_dir, "ic_launcher.png")
        square.resize((size, size), Image.Resampling.LANCZOS).save(launcher_png, "PNG")

        # Round icon (circle mask)
        mask = Image.new("L", (size, size), 0)
        from PIL import ImageDraw
        draw = ImageDraw.Draw(mask)
        draw.ellipse((0, 0, size, size), fill=255)
        
        resized = square.resize((size, size), Image.Resampling.LANCZOS)
        round_img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
        round_img.paste(resized, (0, 0), mask=mask)
        round_png = os.path.join(density_dir, "ic_launcher_round.png")
        round_img.save(round_png, "PNG")
        print(f"Saved {dir_name}: {size}x{size} (square & round)")

    # 4. Android Adaptive Icon Foreground
    # 108dp x 108dp -> 432x432 px at xxxhdpi. Safe zone is center 72dp -> 288x288 px.
    adaptive_size = 432
    safe_size = 288
    adaptive_fg = Image.new("RGBA", (adaptive_size, adaptive_size), (0, 0, 0, 0))
    fg_logo = square.resize((safe_size, safe_size), Image.Resampling.LANCZOS)
    offset = (adaptive_size - safe_size) // 2
    adaptive_fg.paste(fg_logo, (offset, offset), fg_logo)

    drawable_dir = os.path.join(android_res, "drawable")
    os.makedirs(drawable_dir, exist_ok=True)
    fg_path = os.path.join(drawable_dir, "ic_launcher_foreground.png")
    adaptive_fg.save(fg_path, "PNG")
    print(f"Saved adaptive foreground: {fg_path}")

    # 5. Android In-App Logo Drawable
    app_logo_path = os.path.join(drawable_dir, "ic_logo.png")
    square.resize((256, 256), Image.Resampling.LANCZOS).save(app_logo_path, "PNG")
    print(f"Saved in-app logo drawable: {app_logo_path}")

    print("All icon assets generated successfully!")

if __name__ == "__main__":
    generate_icons()
