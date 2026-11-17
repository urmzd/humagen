"""Writes a made-up record set to try the records research example on.

usage: python3 make-fixture.py DIR

Six text records of an invented naturalization application. The person,
the numbers and the preparer are invented. The set has three traps on
purpose: a request for evidence that an email mentions and the folder
does not hold, a response whose mailing nothing proves, and a note that
gives a different day for an appointment than its notice does.
"""
import os
import sys

root = sys.argv[1]
os.makedirs(root, exist_ok=True)
records = {
    "01-payment-confirmation.txt": """Online payment confirmation
Date paid: February 26, 2025
Form: N-400, Application for Naturalization (filed online)
Amount: $710.00
Payer: Jordan Example
""",
    "02-receipt-notice.txt": """Form I-797C, Notice of Action
Notice date: March 4, 2025
Case type: N-400, Application for Naturalization
Received date: February 27, 2025
Receipt number: EXA0000000001
Applicant: Jordan Example
We received your application and fee.
""",
    "03-biometrics-notice.txt": """Form I-797C, Notice of Action
Notice date: March 18, 2025
Biometrics appointment
Date and time: April 2, 2025 at 9:00 AM
Location: Application Support Center, Springfield
Receipt number: EXA0000000001
""",
    "04-email-from-preparer.txt": """From: Sam Preparer <sam@preparer.example>
To: Jordan Example
Date: June 10, 2025
Subject: your case

Jordan, the request for evidence dated May 20 asked for your tax
transcripts. We mailed the response on June 6. I will send you the
tracking number when I find it.
""",
    "05-interview-notice.txt": """Form I-797C, Notice of Action
Notice date: August 12, 2025
Interview appointment
Date and time: September 16, 2025 at 1:30 PM
Location: Field Office, Springfield
Receipt number: EXA0000000001
""",
    "06-my-notes.txt": """my notes
- biometrics done 4/3, took 20 minutes
- interview went fine, officer said decision cannot be made yet
- oath?? no letter yet
""",
}
for name, text in records.items():
    with open(os.path.join(root, name), "w") as f:
        f.write(text)
print("wrote", len(records), "records under", root)
