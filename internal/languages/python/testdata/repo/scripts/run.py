from app import UserService
from app.storage import Store


def main():
    service = UserService(Store())
    service.create("x")
    service.remove(None)
    open("x")


def open(path):
    return path
