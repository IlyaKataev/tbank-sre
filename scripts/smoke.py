#!/usr/bin/env python3
"""Exercise a running marketplace through HTTP; requires only Python 3.

Creates uniquely named test users and an archived product. With --secondary-url,
writes on one replica are read on the other. Save --state-file before restarting
the application/database, then use --verify-state to check durable storage.
The state file contains generated test credentials; it is written with mode 0600.
This script never starts/stops containers or deletes application data.
"""

import argparse
import json
import os
from pathlib import Path
import sys
import uuid
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


class CheckFailed(Exception):
    pass


def check(condition, message):
    if not condition:
        raise CheckFailed(message)


class Client:
    def __init__(self, base_url):
        self.base_url = base_url.rstrip("/")

    def request(self, method, path, *, body=None, token=None, status=200):
        data = None if body is None else json.dumps(body).encode()
        headers = {"Accept": "application/json"}
        if data is not None:
            headers["Content-Type"] = "application/json"
        if token:
            headers["Authorization"] = "Bearer " + token
        request = Request(self.base_url + path, data=data, headers=headers, method=method)
        try:
            response = urlopen(request, timeout=10)
        except HTTPError as error:
            response = error
        with response:
            raw = response.read()
            actual_status = response.status
        check(actual_status == status,
              f"{method} {self.base_url}{path}: expected HTTP {status}, got {actual_status}")
        if not raw:
            return None
        try:
            return json.loads(raw)
        except (ValueError, UnicodeDecodeError) as error:
            raise CheckFailed(f"{method} {path}: response is not JSON") from error

    def error(self, method, path, *, status, code, body=None, token=None):
        result = self.request(method, path, body=body, token=token, status=status)
        check(isinstance(result, dict) and result.get("error_code") == code,
              f"{method} {path}: expected error_code {code}")

    def health(self):
        self.request("GET", "/healthz")
        self.request("GET", "/readyz")

    def login(self, email, password):
        result = self.request("POST", "/auth/login", body={"email": email, "password": password})
        check(bool(result.get("access_token")), "Login did not return an access token")
        return result


def verify_state(clients, state):
    for client in clients:
        client.health()
        token = client.login(state["email"], state["password"])["access_token"]
        product = client.request("GET", "/products/" + state["product_id"], token=token)
        for key, expected in state["product"].items():
            check(product.get(key) == expected, f"Persisted product has unexpected {key}")
        order = client.request("GET", "/orders/" + state["order_id"], token=token)
        check(order["status"] == "CANCELED", "Canceled order did not survive restart")
    print("PASS: saved user, product changes and canceled order persisted")


def run_checks(primary, secondary):
    clients = [primary] if primary.base_url == secondary.base_url else [primary, secondary]
    for client in clients:
        client.health()
    print("PASS: public liveness and database readiness")

    run_id = uuid.uuid4().hex[:12]
    password = "Smoke-" + uuid.uuid4().hex
    seller_email = f"smoke-seller-{run_id}@example.com"
    buyer_email = f"smoke-buyer-{run_id}@example.com"

    for invalid in (
        {"email": "invalid-email", "password": password, "role": "USER"},
        {"email": f"short-{run_id}@example.com", "password": "short", "role": "USER"},
        {"email": f"role-{run_id}@example.com", "password": password, "role": "ROOT"},
        {"email": f"admin-{run_id}@example.com", "password": password, "role": "ADMIN"},
    ):
        primary.error("POST", "/auth/register", body=invalid, status=400, code="VALIDATION_ERROR")

    def register(email, role):
        result = primary.request("POST", "/auth/register", status=201,
                                 body={"email": email, "password": password, "role": role})
        check(bool(result.get("access_token")) and bool(result.get("refresh_token")),
              "Registration did not return both tokens")
        return result

    seller = register(seller_email, "SELLER")
    buyer = register(buyer_email, "USER")
    other_seller = register(f"smoke-other-{run_id}@example.com", "SELLER")
    other_buyer = register(f"smoke-other-buyer-{run_id}@example.com", "USER")
    primary.error("POST", "/auth/register", status=409, code="USER_ALREADY_EXISTS",
                  body={"email": buyer_email, "password": password, "role": "USER"})
    primary.error("POST", "/auth/login", status=401, code="INVALID_CREDENTIALS",
                  body={"email": buyer_email, "password": "incorrect-password"})
    primary.error("GET", "/products", status=401, code="TOKEN_INVALID")
    primary.error("GET", "/products", token="invalid.jwt.token", status=401, code="TOKEN_INVALID")
    secondary.login(buyer_email, password)

    refreshed = secondary.request("POST", "/auth/refresh", body={"refresh_token": buyer["refresh_token"]})
    check(bool(refreshed.get("access_token")), "Refresh did not return an access token")
    primary.error("POST", "/auth/refresh", body={"refresh_token": buyer["refresh_token"]},
                  status=401, code="REFRESH_TOKEN_INVALID")
    buyer_token = refreshed["access_token"]
    seller_token = seller["access_token"]
    print("PASS: auth validation, login, shared sessions and refresh-token rotation")

    category = "smoke-" + run_id
    product_body = {"name": "Smoke product " + run_id, "price": 120.5,
                    "stock": 10, "category": category, "description": "HTTP CRUD check"}
    primary.error("POST", "/products", body=product_body, token=buyer_token,
                  status=403, code="ACCESS_DENIED")
    primary.error("POST", "/products", body={**product_body, "price": -1}, token=seller_token,
                  status=400, code="VALIDATION_ERROR")
    product = primary.request("POST", "/products", body=product_body, token=seller_token, status=201)
    product_id = product["id"]
    product_path = "/products/" + product_id
    check(product["status"] == "ACTIVE", "New product is not active")

    fetched = secondary.request("GET", product_path, token=buyer_token)
    for key, expected in product_body.items():
        check(fetched.get(key) == expected, f"Created product has unexpected {key}")
    listing = secondary.request("GET", "/products?" + urlencode({"category": category, "status": "ACTIVE"}),
                                token=buyer_token)
    check([item["id"] for item in listing["items"]] == [product_id], "Product is missing from filtered list")
    check(listing["totalElements"] == 1, "Filtered product count is wrong")

    updated_body = {**product_body, "name": "Updated " + run_id, "price": 75.25, "stock": 8, "status": "ACTIVE"}
    primary.error("PUT", product_path, body=updated_body, token=other_seller["access_token"],
                  status=403, code="ACCESS_DENIED")
    primary.error("DELETE", product_path, token=other_seller["access_token"],
                  status=403, code="ACCESS_DENIED")
    secondary.request("PUT", product_path, body=updated_body, token=seller_token)
    fetched = primary.request("GET", product_path, token=buyer_token)
    for key, expected in updated_body.items():
        check(fetched.get(key) == expected, f"Updated product has unexpected {key}")
    print("PASS: create/read/update, filtered list and seller ownership")

    order = primary.request("POST", "/orders", status=201, token=buyer_token,
                            body={"items": [{"product_id": product_id, "quantity": 2}]})
    check(order["status"] == "CREATED" and order["total_amount"] == 150.5, "Order total or status is wrong")
    stock = secondary.request("GET", product_path, token=buyer_token)["stock"]
    check(stock == 6, "Order did not reserve product stock")
    order_path = "/orders/" + order["id"]
    secondary.error("POST", order_path + "/cancel", token=other_buyer["access_token"],
                    status=403, code="ORDER_OWNERSHIP_VIOLATION")
    canceled = secondary.request("POST", order_path + "/cancel", token=buyer_token)
    check(canceled["status"] == "CANCELED", "Order was not canceled")
    stock = primary.request("GET", product_path, token=buyer_token)["stock"]
    check(stock == 8, "Cancel did not restore reserved stock")
    primary.error("POST", order_path + "/cancel", token=buyer_token,
                  status=409, code="INVALID_STATE_TRANSITION")
    check(primary.request("GET", product_path, token=buyer_token)["stock"] == 8,
          "Repeated cancellation changed stock")
    print("PASS: order creation, stock reservation, cancellation and ownership")

    primary.request("DELETE", product_path, token=seller_token)
    archived = secondary.request("GET", product_path, token=buyer_token)
    check(archived["status"] == "ARCHIVED", "DELETE did not archive the product")
    listing = secondary.request("GET", "/products?" + urlencode({"category": category, "status": "ACTIVE"}),
                                token=buyer_token)
    check(listing["items"] == [] and listing["totalElements"] == 0,
          "Archived product remains in the active list")
    print("PASS: delete archives product and removes it from the active list")
    if len(clients) > 1:
        print("PASS: reads, writes and tokens work across both supplied replicas")

    return {"email": buyer_email, "password": password, "product_id": product_id, "order_id": order["id"],
            "product": {"name": updated_body["name"], "price": 75.25, "stock": 8, "status": "ARCHIVED"}}


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--base-url", default=os.environ.get("BASE_URL", "http://localhost:8080"))
    parser.add_argument("--secondary-url", default=os.environ.get("SECONDARY_URL"),
                        help="Another replica connected to the same PostgreSQL database and JWT secret")
    state_group = parser.add_mutually_exclusive_group()
    state_group.add_argument("--state-file", type=Path, help="Save test credentials/IDs for a later persistence check")
    state_group.add_argument("--verify-state", type=Path, help="Read saved state and verify it survived a restart")
    args = parser.parse_args()
    primary = Client(args.base_url)
    secondary = Client(args.secondary_url or args.base_url)
    try:
        if args.verify_state:
            verify_state([primary, secondary], json.loads(args.verify_state.read_text()))
        else:
            state = run_checks(primary, secondary)
            if args.state_file:
                fd = os.open(args.state_file, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
                with os.fdopen(fd, "w") as stream:
                    os.fchmod(stream.fileno(), 0o600)
                    json.dump(state, stream, indent=2)
                    stream.write("\n")
                print(f"Saved persistence check state to {args.state_file}")
    except (CheckFailed, URLError, TimeoutError, OSError, ValueError, KeyError, TypeError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
