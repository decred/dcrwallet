// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.ComponentModel;
using System.Runtime.CompilerServices;

namespace Paymetheus
{
    class ViewModelBase : INotifyPropertyChanged
    {
        public ViewModelBase(ViewModelBase parent = null)
        {
            _parentViewModel = parent;
        }

        protected readonly ViewModelBase _parentViewModel;

        public event PropertyChangedEventHandler PropertyChanged;

        protected void RaisePropertyChanged([CallerMemberName] string propertyName = null)
        {
            PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(propertyName));
        }

        public void PostMessage(ViewModelMessage message)
        {
            var vm = _parentViewModel;
            while (vm != null)
            {
                if (vm.OnRoutedMessage(this, message))
                    return;
                vm = vm._parentViewModel;
            }
            Console.WriteLine($"Unhandled message {message}"); // TODO: remove debugging
        }

        /// <summary>
        /// Optionally handles a specific message posted by a child view model.
        /// </summary>
        /// <param name="message">Child message to handle.</param>
        /// <returns>Returns true if the message was handled and should not be propigated to a parent view model.</returns>
        protected virtual bool OnRoutedMessage(ViewModelBase sender, ViewModelMessage message)
        {
            return false;
        }
    }
}
