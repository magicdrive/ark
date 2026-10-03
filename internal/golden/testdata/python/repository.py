from user import User


class Repository:
    def find(self, id):
        u = User()
        u.id = id
        return u
