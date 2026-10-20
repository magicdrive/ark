from . import models
from .models import User
from .storage import Store as Backend
import app.util


def audited(f):
    return f


class UserService:
    def __init__(self, store: Backend):
        self.store = store

    def create(self, name) -> User:
        user = User(name)
        user.save()
        self.store.put(user)
        app.util.log("created")
        return models.User(name)

    @staticmethod
    def helper(user):
        return user.validate()

    @audited
    def remove(self, user):
        self.store.drop(user)
