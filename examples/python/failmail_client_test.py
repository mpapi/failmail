import logging
import logging.handlers


def _configure_logging():
    mail_handler = logging.handlers.SMTPHandler(
        ("localhost", 2525),
        "test@example.com",
        ["errors@example.com"],
        "error from the Python logging example")
    mail_handler.setLevel(logging.ERROR)

    logging.root.addHandler(mail_handler)
    logging.root.setLevel(logging.DEBUG)


def main():
    _configure_logging()
    try:
        1 / 0
    except Exception, e:
        logging.exception("an error has occurred: %s", e)


if __name__ == '__main__':
    main()
