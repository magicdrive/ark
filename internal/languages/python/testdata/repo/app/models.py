from dataclasses import dataclass


class Base:
    def save(self):
        return self._write()

    def _write(self):
        return True


@dataclass
class User(Base):
    name: str

    def save(self):
        super().save()
        return self.validate()

    def validate(self):
        return bool(self.name)
