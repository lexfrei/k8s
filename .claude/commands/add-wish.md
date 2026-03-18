# Add a wish to wish-operator

Add a new Wish manifest to `manifests/wish-operator/`.

## Arguments: $ARGUMENTS

The argument is the product name to add (e.g. "Logitech MX Brio 4K").

## Instructions

1. **Check for duplicates**: Search `manifests/wish-operator/` for existing manifests matching this product. If found, inform the user and stop.

2. **Research the product** using web search:
   - Find the official product page URL
   - Find a clean product image (prefer PNG with transparent background from official sources)
   - Find a purchase URL on ozon.ru (search web, do NOT fetch ozon.ru directly — it hangs)
   - Find the current price in Russian rubles. If price cannot be found, ask the user.

3. **Verify the image URL** works and check transparency:
   - Check HTTP headers with `curl --silent --head --location`. Confirm HTTP 200 and valid image content-type.
   - Download the image and verify transparency with ImageMagick + Python:
     ```bash
     curl --silent --location "IMAGE_URL" | python3 -c "
     from PIL import Image
     import sys, io
     img = Image.open(io.BytesIO(sys.stdin.buffer.read())).convert('RGBA')
     alpha = img.getchannel('A')
     w, h = img.size
     # Check multiple points for real transparency (not just edge pixels)
     opaque_bg = 0
     for y in [0, h//4, h//2, 3*h//4, h-1]:
         for x in [0, w//4, w//2, 3*w//4, w-1]:
             a = alpha.getpixel((x, y))
             r, g, b = img.getpixel((x, y))[:3]
             if a == 255 and r > 240 and g > 240 and b > 240:
                 opaque_bg += 1
     transparent = sum(1 for p in alpha.getdata() if p < 255)
     total = w * h
     print(f'Size: {w}x{h}, Mode: {img.mode}')
     print(f'Transparent pixels: {transparent}/{total} ({transparent*100/total:.1f}%)')
     print(f'White opaque sample points: {opaque_bg}/25')
     if transparent == 0:
         print('VERDICT: No transparency (solid background)')
     elif opaque_bg > 10:
         print('VERDICT: Fake transparency (alpha exists but background is white opaque)')
     else:
         print('VERDICT: Real transparency')
     "
     ```
   - Report the verdict to the user. Prefer images with real transparency.
   - JPEG images never have transparency — this is expected, not an error.

4. **Ask the user to confirm** all details before creating the file:
   - Title
   - Image URL (and what it looks like: color, angle)
   - Official URL
   - Purchase URL(s)
   - Price
   - Tags
   - Priority (default: 3)

5. **Create the manifest** following this format:

```yaml
apiVersion: wishlist.k8s.lex.la/v1alpha1
kind: Wish
metadata:
  name: <kebab-case-product-name>
  namespace: wish-operator
spec:
  title: "<Product Name (Variant)>"
  imageURL: "<direct image URL>"
  officialURL: "<official product page>"
  purchaseURLs:
    - "<ozon or other store URL>"
  msrp: "₽ <price>"
  tags:
    - <category tags>
  contextTags:
    - any-occasion
  description: "<1-2 sentence product description with key specs>"
  priority: 3
```

6. **File naming**: `manifests/wish-operator/<kebab-case-product-name>.yaml`

## Important notes

- Do NOT fetch ozon.ru pages directly (WebFetch hangs on Ozon). Use web search to find Ozon links.
- Prefer official product images from manufacturer CDN.
- Image should ideally have transparent or white background.
- Description should be concise — key specs only, no marketing fluff.
- All content in English (title, description, tags).
- Price in rubles with ₽ symbol and space: `"₽ 13500"`.
