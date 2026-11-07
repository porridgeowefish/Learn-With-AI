#import <Foundation/Foundation.h>
#import <Speech/Speech.h>

// Embedded Info.plist gives this helper its own speech privacy description.
// Recognition always requires an on-device engine; no Apple cloud fallback.
static void output(NSDictionary *result) {
    NSData *data = [NSJSONSerialization dataWithJSONObject:result options:0 error:nil];
    if (data) [[NSFileHandle fileHandleWithStandardOutput] writeData:data];
}
static BOOL waitUntil(BOOL (^finished)(void), NSTimeInterval seconds) {
    NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:seconds];
    while (!finished() && deadline.timeIntervalSinceNow > 0) {
        [[NSRunLoop currentRunLoop] runUntilDate:[NSDate dateWithTimeIntervalSinceNow:0.05]];
    }
    return finished();
}
int main(void) {
    @autoreleasepool {
        NSData *input = [[NSFileHandle fileHandleWithStandardInput] readDataToEndOfFile];
        NSDictionary *request = [NSJSONSerialization JSONObjectWithData:input options:0 error:nil];
        if (![request isKindOfClass:[NSDictionary class]]) return 2;
        NSMutableArray *languages = [NSMutableArray array];
        for (NSString *language in @[@"zh-CN", @"en-US"]) {
            SFSpeechRecognizer *recognizer = [[SFSpeechRecognizer alloc] initWithLocale:[NSLocale localeWithLocaleIdentifier:language]];
            if (recognizer && recognizer.supportsOnDeviceRecognition) [languages addObject:language];
        }
        if ([request[@"mode"] isEqual:@"probe"]) { output(@{@"languages":languages}); return 0; }
        NSString *language = request[@"language"];
        if (![language isEqual:@"zh-CN"] && ![language isEqual:@"en-US"]) return 2;
        if (![languages containsObject:language]) { output(@{@"error":@"on-device-unavailable"}); return 0; }
        __block BOOL authorized = NO;
        __block SFSpeechRecognizerAuthorizationStatus status = SFSpeechRecognizerAuthorizationStatusNotDetermined;
        [SFSpeechRecognizer requestAuthorization:^(SFSpeechRecognizerAuthorizationStatus value) { status = value; authorized = YES; }];
        if (!waitUntil(^BOOL { return authorized; }, 30)) { output(@{@"error":@"timeout"}); return 0; }
        if (status != SFSpeechRecognizerAuthorizationStatusAuthorized) { output(@{@"error":@"permission-denied"}); return 0; }
        if (![request[@"path"] isKindOfClass:[NSString class]]) return 2;
        SFSpeechRecognizer *recognizer = [[SFSpeechRecognizer alloc] initWithLocale:[NSLocale localeWithLocaleIdentifier:language]];
        SFSpeechURLRecognitionRequest *recognition = [[SFSpeechURLRecognitionRequest alloc] initWithURL:[NSURL fileURLWithPath:request[@"path"]]];
        recognition.requiresOnDeviceRecognition = YES;
        recognition.shouldReportPartialResults = NO;
        __block BOOL done = NO;
        __block NSString *text = @"";
        __block float confidence = 0;
        __block BOOL failed = NO;
        SFSpeechRecognitionTask *task = [recognizer recognitionTaskWithRequest:recognition resultHandler:^(SFSpeechRecognitionResult *result, NSError *error) {
            if (result.isFinal) {
                text = result.bestTranscription.formattedString ?: @"";
                float sum = 0;
                for (SFTranscriptionSegment *segment in result.bestTranscription.segments) sum += segment.confidence;
                NSUInteger count = result.bestTranscription.segments.count;
                confidence = count ? sum / count : 0;
                done = YES;
            }
            if (error) { failed = YES; done = YES; }
        }];
        if (!waitUntil(^BOOL { return done; }, 65)) { [task cancel]; output(@{@"error":@"timeout"}); return 0; }
        // Apple's no-speech result is surfaced as an empty transcription.
        output(failed ? @{@"error":@"recognition-failed"} : @{@"text":text, @"confidence":@(confidence)});
    }
    return 0;
}
