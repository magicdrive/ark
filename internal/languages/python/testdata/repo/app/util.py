def log(message):
    print(message)


def _format(message):
    def wrap(text):
        return "[" + text + "]"

    return wrap(message)
