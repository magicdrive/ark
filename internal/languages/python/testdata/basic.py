import os
import sys
from typing import Optional
from pathlib import Path

DEFAULT_TIMEOUT = 5000


class UserService:
    def __init__(self, repo):
        self.repo = repo

    def create(self, name: str):
        user = User(name)
        self.repo.save(user)
        return user

    async def async_create(self, name: str):
        user = User(name)
        await self.repo.async_save(user)
        return user


class User:
    def __init__(self, name: str):
        self.name = name
        self.id = os.urandom(16).hex()

    def __repr__(self):
        return f"User({self.name!r})"


def format_user(u: "User") -> str:
    return f"{u.name} ({u.id})"


def _private_helper():
    pass
