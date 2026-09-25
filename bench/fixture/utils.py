TAX_RATE = 0.13


def parse_sku(raw):
    letter, number = raw.strip().upper().split("-")
    return f"{letter}-{int(number)}"


def price_with_tax(price):
    return round(price * (1 + TAX_RATE), 2)
