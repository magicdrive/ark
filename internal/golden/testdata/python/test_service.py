from service import UserService


def test_create():
    s = UserService()
    s.create("alice")
