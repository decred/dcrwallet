// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information

using System;
using System.Collections.Generic;
using System.Windows;

namespace Paymetheus.Framework
{
    public static class SingletonViewModelLocator
    {
        static Dictionary<string, Func<ViewModelBase>> Factories = new Dictionary<string, Func<ViewModelBase>>();
        static Dictionary<string, ViewModelBase> ViewModels = new Dictionary<string, ViewModelBase>();

        public static void RegisterFactory<TView>(Func<ViewModelBase> factory)
        {
            Factories[typeof(TView).Name] = factory;
        }

        public static void RegisterFactory<TView, TViewModel>()
            where TView : FrameworkElement
            where TViewModel : ViewModelBase, new()
        {
            RegisterFactory<TView>(() => new TViewModel());
        }

        public static void RegisterInstance<TView>(ViewModelBase viewModel)
            where TView : FrameworkElement
        {
            ViewModels[typeof(TView).Name] = viewModel;
        }

        public static void RegisterInstance(string name, ViewModelBase viewModel)
        {
            ViewModels[name] = viewModel;
        }

        public static ViewModelBase Resolve(string viewTypeName)
        {
            if (ViewModels.ContainsKey(viewTypeName))
            {
                return ViewModels[viewTypeName];
            }

            if (Factories.ContainsKey(viewTypeName))
            {
                var instance = Factories[viewTypeName]();
                ViewModels[viewTypeName] = instance;
                return instance;
            }

            return null;
        }
    }
}
