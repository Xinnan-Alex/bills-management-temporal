package bills

BillCloseTimeout: int64 | *60

TemporalServer: [
    if #Meta.Environment.Cloud == "local" { "localhost:7233" },
    "quickstart-leongxinn-2cef88fa.bsl4y.tmprl.cloud:7233",
][0]

NameSpace: [
    if #Meta.Environment.Cloud == "local" { "default" },
    "quickstart-leongxinn-2cef88fa.bsl4y",
][0]