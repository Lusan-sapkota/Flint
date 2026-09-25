from utils import parse_sku, price_with_tax

MAX_RETRIES = 4


def handle_order(order):
    sku = parse_sku(order["sku"])
    for attempt in range(MAX_RETRIES):
        if reserve(sku, order["quantity"]):
            return price_with_tax(order["price"]) * order["quantity"]
    raise RuntimeError(f"could not reserve {sku}")


def reserve(sku, quantity):
    return quantity <= 100
