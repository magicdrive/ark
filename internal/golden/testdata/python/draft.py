# Draft also declares save() so that save() calls have more than one
# candidate (ambiguity test).
class Draft:
    def save(self):
        return None
