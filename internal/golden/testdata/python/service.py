from repository import Repository
from user import User


class UserService:
    def __init__(self):
        self.repo = Repository()

    def create(self, name):
        u = User()
        u.name = name
        found = self.repo.find(u.id)
        found.save()
