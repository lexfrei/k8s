# Wish CRD Reference

## Minimal Example

```yaml
apiVersion: wishlist.lex.la/v1alpha1
kind: Wish
metadata:
  name: my-wish
  namespace: wish-operator
spec:
  title: "Item Name"
```

## Full Example

```yaml
apiVersion: wishlist.lex.la/v1alpha1
kind: Wish
metadata:
  name: mechanical-keyboard
  namespace: wish-operator
spec:
  title: "Keychron Q1 Pro"
  imageURL: "https://example.com/keyboard.jpg"
  officialURL: "https://keychron.com/q1-pro"
  purchaseURLs:
    - "https://amazon.com/..."
    - "https://aliexpress.com/..."
  msrp: "₽ 19900"
  tags:
    - electronics
    - keyboards
  contextTags:
    - birthday
    - christmas
  description: "Need a new keyboard for work"
  priority: 4        # 1-5 stars
  ttl: "8760h"       # 1 year (optional, wish expires after this)
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | yes | Name of the item |
| `imageURL` | string | | URL to product image |
| `officialURL` | string | | Link to official product page |
| `purchaseURLs` | []string | | Where to buy |
| `msrp` | string | | Price display (e.g., "₽ 19900") |
| `tags` | []string | | Category labels |
| `contextTags` | []string | | Occasions (birthday, christmas) |
| `description` | string | | Why you want it |
| `priority` | int (0-5) | | Importance (displayed as stars) |
| `ttl` | duration | | How long wish stays active |

## Status (read-only)

| Field | Description |
|-------|-------------|
| `reserved` | Someone reserved this wish |
| `reservedAt` | When it was reserved |
| `reservationExpires` | When reservation expires (1-8 weeks) |
| `active` | Within TTL |
