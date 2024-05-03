## usage

```
logs.Dubug(something)
logs.Info(something)
logs.Error(something)
...
```

## change default config

```
conf := logs.GetLogConf()
conf.MaxAge = 30
logs.InitLogSetting(conf)
```

## License

logs is released under the MIT License. For more information, see the [LICENSE](LICENSE) file.
