from pathlib import Path
from zipfile import ZipFile, ZIP_DEFLATED
import json
from PIL import Image
from reportlab.pdfgen.canvas import Canvas
from reportlab.lib.pagesizes import landscape, A4
from reportlab.lib import colors
from reportlab.platypus import Paragraph, Table, TableStyle
from reportlab.lib.styles import ParagraphStyle
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont

root=Path(__file__).resolve().parent
for name,file in [('Review','DejaVuSans.ttf'),('ReviewBold','DejaVuSans-Bold.ttf')]:
    pdfmetrics.registerFont(TTFont(name,'/usr/share/fonts/truetype/dejavu/'+file))
W,H=landscape(A4)
ink=colors.HexColor('#1b1c1e');muted=colors.HexColor('#626770');accent=colors.HexColor('#2e5b88');line=colors.HexColor('#e0e3e5')
styles={
 'body':ParagraphStyle('Body',fontName='Review',fontSize=10,leading=15,textColor=ink),
 'small':ParagraphStyle('Small',fontName='Review',fontSize=8.5,leading=12,textColor=muted),
 'head':ParagraphStyle('Head',fontName='ReviewBold',fontSize=12,leading=17,textColor=ink),
 'cell':ParagraphStyle('Cell',fontName='Review',fontSize=9,leading=13,textColor=ink),
}
pdf=root/'Humagen-interaction-system.pdf'
c=Canvas(str(pdf),pagesize=(W,H));c.setTitle('Humagen — Conversation, work, and deliverables');c.setAuthor('Humagen design concept')
def text(value,x,y,width,style='body'):
    p=Paragraph(value,styles[style]);_,h=p.wrap(width,H);p.drawOn(c,x,y-h);return y-h
def mark(x,y):
    c.saveState();c.translate(x+8,y+8);c.rotate(8);c.setFillColor(ink)
    for row in range(2):
        for col in range(2):c.roundRect(-8+col*10,-8+row*10,7,7,2,fill=1,stroke=0)
    c.restoreState()
def header(title,subtitle,page):
    mark(33,H-36);c.setFillColor(ink);c.setFont('ReviewBold',12);c.drawString(58,H-30,'humagen')
    c.setFillColor(muted);c.setFont('Review',8);c.drawRightString(W-32,H-28,'INTERACTION SYSTEM  /  SECOND PASS')
    c.setFillColor(ink);c.setFont('ReviewBold',21);c.drawString(32,H-68,title)
    text(subtitle,32,H-82,W-64,'small')
    c.setStrokeColor(line);c.line(32,32,W-32,32)
    c.setFillColor(muted);c.setFont('Review',8);c.drawString(32,18,'October 23, 2026 · Interactive concept · Sample data and simulated agents')
    c.drawRightString(W-32,18,f'{page} / 7')
def screenshot(name,x,y,w,h):
    p=root/name;im=Image.open(p);scale=min(w/im.width,h/im.height)
    iw,ih=im.width*scale,im.height*scale
    c.drawImage(str(p),x+(w-iw)/2,y+(h-ih)/2,width=iw,height=ih,preserveAspectRatio=True)
def nextpage():c.showPage()

header('A conversation that carries the work.','Messages, agent coordination, decisions, and deliverables share one project home.',1)
y=text('Keep the new logo and the quiet project room. Add a small set of consistent interaction patterns so people can understand what happened, answer what is needed, and use the result without leaving the conversation.',32,H-118,W-64)-18
rows=[['Interaction','What it communicates','Primary action'],
 ['Message','Who is speaking, what they mean, and the message or agent being addressed.','Reply, copy, or ask the agent.'],
 ['Work update','What was checked or changed, the result, and anything blocking progress.','Inspect evidence or a specific tool step.'],
 ['Clarification','A question that resolves uncertainty in one assignment.','Choose and explicitly submit an answer.'],
 ['Approval','The exact action, audience, and content someone is authorizing.','Review and approve or redirect.'],
 ['Artifact','A usable document, deck, or table, with author, version, status, and sources.','Open, present, download, or request changes.']]
table=Table([[Paragraph(v,styles['cell']) for v in row] for row in rows],colWidths=[110,414,W-64-524])
table.setStyle(TableStyle([('BACKGROUND',(0,0),(-1,0),colors.HexColor('#edf2f7')),('VALIGN',(0,0),(-1,-1),'TOP'),('GRID',(0,0),(-1,-1),.5,line),('LEFTPADDING',(0,0),(-1,-1),10),('RIGHTPADDING',(0,0),(-1,-1),10),('TOPPADDING',(0,0),(-1,-1),9),('BOTTOMPADDING',(0,0),(-1,-1),9)]))
_,th=table.wrap(W-64,H);table.drawOn(c,32,y-th);y-=th+20
y=text('What this enables next',32,y,W-64,'head')-7
y=text('An event starts an assignment. Agents work within its scope and return when a person’s judgment is needed. Reviewed outputs become durable project knowledge. Work can resume with its evidence, versions, and decisions intact.',32,y,W-64)-14
total=sum(len(json.loads((root/name).read_text())['checks']) for name in ['core-validation.json','interaction-validation.json','offline-validation.json'])
text(f'Validation: {total} Chromium checks across 320, 390, 768, and 1440 pixel widths, plus file-integrity checks. Open index.html to try the flow. The sample downloads are real; connected execution, persistence, and publishing are future implementation work.',32,y,W-64,'small')
nextpage()

pages=[
 (2,'Tool calls explain the result.','A compact work summary opens into individual steps, with technical detail available when useful.','preview-tool-inspector.png','A step has an action, result, duration, source, and acting person. Inputs and structured outputs live in tabs. A blocked source points to its owner and recovery action.'),
 (3,'Ask one clear question.','An agent can ask for missing direction within the conversation, while other project work keeps its own state.','preview-clarification.png','Writer asks who the briefing is for. Selecting an option does not submit it. The explicit answer unlocks this draft and leaves the launch-date and vendor-message approvals separate.'),
 (4,'Open the result beside the conversation.','A document, presentation, or table is a native, versioned object in the workspace.','preview-artifact.png','The viewer keeps Preview, Versions, and Sources together. Every result links back to its conversation and author. A presentation includes slide navigation, speaker notes, and editable PPTX / PDF downloads.'),
 (5,'Present directly from the workspace.','The same deck moves into a focused presentation view, with keyboard navigation and an easy return.','preview-present.png','Arrow keys navigate the six-slide deck; Escape returns to the workbench. The audience and Internal draft status remain visible. Presentation mode does not publish or distribute the result.'),
 (6,'Feedback keeps its exact context.','Direct requests to an agent and revisions to an artifact stay in the conversation.','preview-revision.png','Request Changes attaches the artifact, version, and selected slide to the composer. Sending records that reference and marks the draft Changes requested. The delivered version stays intact until a future revision is created.'),
]
for number,title,subtitle,file,caption in pages:
    header(title,subtitle,number);screenshot(file,32,70,W-64,H-184);text(caption,32,60,W-64,'small');nextpage()

header('One home, across screens.','Project deliverables remain easy to find; focused panels adapt to smaller screens.',7)
screenshot('preview-phone-artifact.png',32,72,242,H-185)
screenshot('preview-library.png',299,215,W-331,H-328)
y=text('Try the whole loop',299,195,W-331,'head')-9
for value in [
 'Open Needs you and answer Writer’s audience question.',
 'Open Launch briefing, inspect its evidence, and present it.',
 'Request a change to one slide, then find the same result in Deliverables.'
]:y=text('• '+value,299,y,W-331)-6
y-=5
text('On phone, the artifact takes focus and the background becomes inactive. Tab stays inside the viewer; Escape returns to its opener. Requesting a revision returns to the scoped composer.',299,y,W-331,'small')
c.save()
bundle=root/'Humagen-interaction-system.zip'
with ZipFile(bundle,'w',ZIP_DEFLATED) as z:
    for p in sorted(root.rglob('*')):
        if p.is_file() and p != bundle and not p.name.startswith(('validation-failure','core-preview-')):
            z.write(p,arcname='Humagen-interaction-system/'+str(p.relative_to(root)))
print(f'Created {pdf} ({total} browser checks)')
print(f'Created {bundle}')
